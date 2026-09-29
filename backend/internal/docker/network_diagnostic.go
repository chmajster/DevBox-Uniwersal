package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
	"io"
	"strconv"
	"strings"
	"time"
)

// Scripts run in the application container's network AND filesystem namespaces,
// so Docker DNS and /etc/hosts (including host-gateway) are the actual app's.
// Each interpreter receives host/port as arguments, never shell interpolation.
const phpNetworkProbe = `$h=$argv[1];$p=(int)$argv[2];$a=@gethostbynamel($h);$r=["dns_resolved"=>false,"tcp_reachable"=>false,"addresses"=>[]];if(filter_var($h,FILTER_VALIDATE_IP)){$a=[$h];}if($a){$r["dns_resolved"]=true;$r["addresses"]=$a;$s=@stream_socket_client("tcp://".(strpos($h,":")!==false?"[".$h."]":$h).":".$p,$e,$m,3);if($s){$r["tcp_reachable"]=true;fclose($s);}}echo json_encode($r);`
const pythonNetworkProbe = `import socket,sys,json
r={"dns_resolved":False,"tcp_reachable":False,"addresses":[]}
try:
 a=socket.getaddrinfo(sys.argv[1],int(sys.argv[2]),type=socket.SOCK_STREAM);r["dns_resolved"]=True;r["addresses"]=list(dict.fromkeys(x[4][0] for x in a));s=socket.create_connection((sys.argv[1],int(sys.argv[2])),3);s.close();r["tcp_reachable"]=True
except OSError: pass
print(json.dumps(r))`
const nodeNetworkProbe = `const dns=require('node:dns'),net=require('node:net');let done=false;const r={dns_resolved:false,tcp_reachable:false,addresses:[]};function end(){if(done)return;done=true;console.log(JSON.stringify(r));process.exit(0)};setTimeout(end,5000);dns.lookup(process.argv[1],{all:true},(e,a)=>{if(e)return end();r.dns_resolved=true;r.addresses=a.map(x=>x.address);const s=net.connect({host:process.argv[1],port:Number(process.argv[2])});s.setTimeout(3000);s.on('connect',()=>{r.tcp_reachable=true;s.destroy();end()});s.on('error',end);s.on('timeout',()=>{s.destroy();end()})});`

func (p *CLIProvider) TestApplicationNetwork(parent context.Context, target providers.ApplicationNetworkTarget) (providers.NetworkDiagnostic, error) {
	report := providers.NetworkDiagnostic{Host: target.Host, Port: target.Port}
	if target.Port < 1 || target.Port > 65535 || target.Host == "" || strings.ContainsAny(target.Host, "\x00\r\n /\\") {
		return report, fmt.Errorf("invalid database network endpoint")
	}
	if err := validateValue(target.ProjectID, "project ID"); err != nil {
		return report, err
	}
	ctx, cancel := context.WithTimeout(parent, 4*time.Minute)
	defer cancel()
	started := time.Now()
	out, _, err := p.runner.Run(ctx, "container", "ls", "--filter", "status=running", "--filter", "label=io.devbox.project="+target.ProjectID, "--format", "{{.ID}}")
	if err != nil {
		return report, err
	}
	ids := strings.Fields(string(out))
	if len(ids) == 0 {
		if err := validateProjectName(target.ProjectName); err != nil {
			return report, err
		}
		service := target.ApplicationService
		if service == "" {
			service, err = p.detectComposeApplicationService(ctx, target.Directory, target.ProjectName)
			if err != nil {
				return report, fmt.Errorf("deploy the application before testing its network: %w", err)
			}
		}
		if err := validateServiceName(service); err != nil {
			return report, err
		}
		out, _, err = p.runner.Run(ctx, "container", "ls", "--filter", "status=running", "--filter", "label=com.docker.compose.project="+target.ProjectName, "--filter", "label=com.docker.compose.service="+service, "--format", "{{.ID}}")
		if err != nil {
			return report, err
		}
		ids = strings.Fields(string(out))
	}
	if len(ids) != 1 {
		return report, fmt.Errorf("network test requires exactly one running application container; found %d", len(ids))
	}
	id := ids[0]
	if err := validateContainerRef(id); err != nil {
		return report, err
	}
	report.Container = id
	host := strings.Trim(target.Host, "[]")
	port := strconv.Itoa(target.Port)
	probes := [][]string{{"php", "-r", phpNetworkProbe, "--", host, port}, {"python3", "-c", pythonNetworkProbe, host, port}, {"python", "-c", pythonNetworkProbe, host, port}, {"node", "-e", nodeNetworkProbe, host, port}}
	for _, probe := range probes {
		attempt, stop := context.WithTimeout(ctx, 6*time.Second)
		out, _, err = p.runner.Run(attempt, append([]string{"container", "exec", id}, probe...)...)
		stop()
		if err != nil {
			if ctx.Err() != nil {
				return report, ctx.Err()
			}
			continue
		}
		if err = json.Unmarshal(out, &report); err != nil {
			continue
		}
		report.Container = id
		report.Host = target.Host
		report.Port = target.Port
		report.DurationMS = time.Since(started).Milliseconds()
		switch {
		case report.TCPReachable:
			report.Message = "DNS and TCP succeeded from the application container; SQL credentials were not tested"
		case !report.DNSResolved:
			report.Message = "Host resolution failed inside the application container; redeploy to apply host-gateway mapping"
		default:
			report.Message = "DNS resolved, but TCP failed; check the server listener, bind-address, firewall and selected port"
		}
		return report, nil
	}
	// A minimal/custom image need not ship a scripting interpreter. A disposable
	// helper joins the EXACT application network namespace, with its hosts-file
	// snapshot and an explicit resolver-config check. It never changes the app.
	out, err = p.networkHelperProbe(ctx, id, host, port)
	if err != nil {
		return report, err
	}
	if err := json.Unmarshal(out, &report); err != nil {
		return report, fmt.Errorf("invalid diagnostic helper response")
	}
	report.Container = id
	report.Host = target.Host
	report.Port = target.Port
	report.DurationMS = time.Since(started).Milliseconds()
	report.Message = "Credential-free test using the application network namespace; SQL authentication was not tested"
	return report, nil
}

// Docker cp's archive is read in memory. No archive path is extracted to disk.
func (p *CLIProvider) containerResolverFile(ctx context.Context, id, name string) (string, error) {
	raw, _, err := p.runner.Run(ctx, "container", "cp", id+":"+name, "-")
	if err != nil {
		return "", fmt.Errorf("read application resolver configuration: %w", err)
	}
	if len(raw) > 128*1024 {
		return "", fmt.Errorf("resolver archive exceeds limit")
	}
	tr := tar.NewReader(bytes.NewReader(raw))
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return "", e
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA {
			continue
		}
		if h.Size > 32768 {
			return "", fmt.Errorf("resolver file exceeds limit")
		}
		content, e := io.ReadAll(io.LimitReader(tr, 32769))
		if e != nil {
			return "", e
		}
		return base64.StdEncoding.EncodeToString(content), nil
	}
	return "", fmt.Errorf("resolver archive contained no regular file")
}
func (p *CLIProvider) networkHelperProbe(ctx context.Context, id, host, port string) ([]byte, error) {
	hosts, err := p.containerResolverFile(ctx, id, "/etc/hosts")
	if err != nil {
		return nil, err
	}
	resolver, err := p.containerResolverFile(ctx, id, "/etc/resolv.conf")
	if err != nil {
		return nil, err
	}
	const image = "python:3.12-alpine"
	if _, _, err := p.runner.Run(ctx, "image", "inspect", image); err != nil {
		if _, _, err = p.runner.Run(ctx, "image", "pull", image); err != nil {
			return nil, fmt.Errorf("pull network diagnostic image: %w", err)
		}
	}
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	name := "devbox-netcheck-" + hex.EncodeToString(nonce)
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _, _ = p.runner.Run(cleanup, "container", "rm", "-f", name)
	}()
	probe, stop := context.WithTimeout(ctx, 20*time.Second)
	defer stop()
	out, _, err := p.runner.Run(probe, "run", "--rm", "--name", name, "--network", "container:"+id, "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--pids-limit", "32", "--memory", "64m", "--cpus", "0.25", "--entrypoint", "python3", image, "-c", helperNetworkProbe, host, port, hosts, resolver)
	if err != nil {
		return nil, fmt.Errorf("network namespace diagnostic failed: %w", err)
	}
	return out, nil
}

const helperNetworkProbe = `import socket,sys,json,base64
r={"dns_resolved":False,"tcp_reachable":False,"addresses":[]}
host=sys.argv[1];port=int(sys.argv[2]);hosts=base64.b64decode(sys.argv[3]).decode();resolver=base64.b64decode(sys.argv[4]).decode()
def normalize(s): return [" ".join(x.split()) for line in s.splitlines() if (x:=line.split("#",1)[0].strip())]
if normalize(resolver)!=normalize(open("/etc/resolv.conf").read()): raise SystemExit("Resolver configuration differs from application")
addresses=[]
for line in hosts.splitlines():
 fields=line.split("#",1)[0].split()
 if len(fields)>1 and host.lower().rstrip(".") in [x.lower().rstrip(".") for x in fields[1:]]: addresses.append(fields[0])
try:
 if not addresses: addresses=[a[4][0] for a in socket.getaddrinfo(host,port,type=socket.SOCK_STREAM)]
 r["addresses"]=list(dict.fromkeys(addresses));r["dns_resolved"]=bool(addresses)
 for address in r["addresses"][:4]:
  try: s=socket.create_connection((address,port),3);s.close();r["tcp_reachable"]=True;break
  except OSError: pass
except OSError: pass
print(json.dumps(r))`
