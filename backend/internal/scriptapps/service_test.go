package scriptapps

import "testing"

func TestNormalizeSourceURL(t *testing.T){
	got,err:=normalizeSource("https://example.com/install.sh",false)
	if err!=nil{t.Fatal(err)}
	if got!="https://example.com/install.sh"{t.Fatalf("got %q",got)}
}
func TestNormalizeSourceCurl(t *testing.T){
	got,err:=normalizeSource("curl -fsSL https://example.com/install.sh | bash",false)
	if err!=nil{t.Fatal(err)}
	if got!="https://example.com/install.sh"{t.Fatalf("got %q",got)}
}
func TestNormalizeSourceRejectsHTTPByDefault(t *testing.T){
	if _,err:=normalizeSource("http://example.com/install.sh",false);err==nil{t.Fatal("expected insecure HTTP to be rejected")}
}
func TestNormalizeSourceAllowsLoopbackHTTP(t *testing.T){
	if _,err:=normalizeSource("http://127.0.0.1:8080/install.sh",false);err!=nil{t.Fatal(err)}
}
func TestNormalizeSourceRejectsURLCredentials(t *testing.T){
	if _,err:=normalizeSource("https://user:pass@example.com/install.sh",false);err==nil{t.Fatal("expected URL credentials to be rejected")}
}
