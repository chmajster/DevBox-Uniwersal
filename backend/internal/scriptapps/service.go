package scriptapps

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
)

var ErrInvalidInput = errors.New("invalid script app input")

type Service struct {
	repo *Repository
	jobs jobs.JobRunner
}

func NewService(repo *Repository, runner jobs.JobRunner)*Service{return &Service{repo:repo,jobs:runner}}

func (s *Service) List(ctx context.Context)([]App,error){return s.repo.List(ctx)}
func (s *Service) Get(ctx context.Context,id string)(App,error){return s.repo.Get(ctx,id)}

func (s *Service) Create(ctx context.Context,in CreateInput,actor *string)(App,*domain.Job,error){
	in.Name=strings.TrimSpace(in.Name); if in.Name==""||len(in.Name)>120{return App{},nil,fmt.Errorf("%w: name is required and must be at most 120 characters",ErrInvalidInput)}
	source,err:=normalizeSource(in.InstallSource,in.AllowInsecure);if err!=nil{return App{},nil,err}
	update:="";if strings.TrimSpace(in.UpdateSource)!=""{update,err=normalizeSource(in.UpdateSource,in.AllowInsecure);if err!=nil{return App{},nil,fmt.Errorf("%w: update source: %v",ErrInvalidInput,err)}}
	uninstall:="";if strings.TrimSpace(in.UninstallSource)!=""{uninstall,err=normalizeSource(in.UninstallSource,in.AllowInsecure);if err!=nil{return App{},nil,fmt.Errorf("%w: uninstall source: %v",ErrInvalidInput,err)}}
	interpreter:=strings.ToLower(strings.TrimSpace(in.Interpreter));if interpreter==""{interpreter="bash"}
	switch interpreter{case "bash","sh","pwsh","powershell":default:return App{},nil,fmt.Errorf("%w: unsupported interpreter",ErrInvalidInput)}
	checksum:=strings.ToLower(strings.TrimSpace(in.ChecksumSHA256))
	if checksum!=""&&!regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(checksum){return App{},nil,fmt.Errorf("%w: checksum_sha256 must contain 64 hexadecimal characters",ErrInvalidInput)}
	manager,target:="script",""
	if v:=strings.TrimSpace(in.ServiceName);v!=""{manager="systemd";target=strings.TrimSuffix(v,".service")+".service"}
	now:=time.Now().UTC()
	a:=App{ID:newID(),Name:in.Name,Description:strings.TrimSpace(in.Description),InstallSource:source,UpdateSource:update,UninstallSource:uninstall,
		Interpreter:interpreter,ChecksumSHA256:checksum,RunAsRoot:in.RunAsRoot,AllowInsecure:in.AllowInsecure,Manager:manager,ManagerTarget:target,
		Status:"not_installed",CreatedBy:actor,CreatedAt:now,UpdatedAt:now}
	if err:=s.repo.Create(ctx,a);err!=nil{return App{},nil,err}
	if !in.InstallNow{return a,nil,nil}
	j,err:=s.enqueue(ctx,a.ID,JobInstall,actor);if err!=nil{return a,nil,err};return a,&j,nil
}

func (s *Service) Delete(ctx context.Context,id string)error{
	a,err:=s.repo.Get(ctx,id);if err!=nil{return err}
	if a.Status!="not_installed"&&a.Status!="uninstalled"{return fmt.Errorf("%w: uninstall the application before deleting its record",ErrInvalidInput)}
	return s.repo.Delete(ctx,id)
}

func (s *Service) Action(ctx context.Context,id,action string,actor *string)(domain.Job,error){
	a,err:=s.repo.Get(ctx,id);if err!=nil{return domain.Job{},err}
	var typ string
	switch action{
	case "install":typ=JobInstall
	case "update":if a.UpdateSource==""{return domain.Job{},fmt.Errorf("%w: update_source is not configured",ErrInvalidInput)};typ=JobUpdate
	case "uninstall":if a.UninstallSource==""{return domain.Job{},fmt.Errorf("%w: uninstall_source is not configured",ErrInvalidInput)};typ=JobUninstall
	case "start":typ=JobStart
	case "stop":typ=JobStop
	case "restart":typ=JobRestart
	default:return domain.Job{},fmt.Errorf("%w: unsupported action",ErrInvalidInput)
	}
	if (action=="start"||action=="stop"||action=="restart")&&(a.Manager=="script"||a.ManagerTarget==""){return domain.Job{},fmt.Errorf("%w: no manageable systemd service or Docker container was detected",ErrInvalidInput)}
	return s.enqueue(ctx,id,typ,actor)
}

func(s *Service) enqueue(ctx context.Context,id,typ string,actor *string)(domain.Job,error){
	return s.jobs.Enqueue(ctx,jobs.Request{Type:typ,RequestedBy:actor,Payload:map[string]any{"script_app_id":id}})
}

func normalizeSource(raw string,allowInsecure bool)(string,error){
	raw=strings.TrimSpace(raw);if raw==""{return "",fmt.Errorf("%w: installer URL or curl command is required",ErrInvalidInput)}
	if strings.HasPrefix(raw,"curl "){
		fields:=strings.Fields(raw);for i:=len(fields)-1;i>=1;i--{if strings.HasPrefix(fields[i],"http://")||strings.HasPrefix(fields[i],"https://"){raw=strings.Trim(fields[i],"'\"");break}}
	}
	u,err:=url.Parse(raw);if err!=nil||u.Host==""||(u.Scheme!="https"&&u.Scheme!="http"){return "",fmt.Errorf("%w: source must be an HTTP(S) URL or curl command containing one",ErrInvalidInput)}
	if u.User!=nil{return "",fmt.Errorf("%w: credentials in installer URL are not allowed",ErrInvalidInput)}
	if u.Scheme=="http"&&!allowInsecure&&!isLoopbackHost(u.Hostname()){return "",fmt.Errorf("%w: plain HTTP requires allow_insecure",ErrInvalidInput)}
	return u.String(),nil
}
func isLoopbackHost(host string)bool{
	if strings.EqualFold(host,"localhost"){return true};ip:=net.ParseIP(host);return ip!=nil&&ip.IsLoopback()
}
