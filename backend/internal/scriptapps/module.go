package scriptapps

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/audit"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

type Module struct{service *Service;audit *audit.Service}
func NewModule(service *Service,auditService *audit.Service)*Module{return &Module{service:service,audit:auditService}}
func(m *Module)Name()string{return "scriptapps"}

func(m *Module)RegisterRoutes(mux *http.ServeMux,mw api.ModuleMiddleware){
	secure:=func(role domain.Role,h http.HandlerFunc)http.Handler{return mw.Authenticate(mw.RequireRole(role,h))}
	mux.Handle("GET /api/v1/script-apps",secure(domain.RoleViewer,m.list))
	mux.Handle("POST /api/v1/script-apps",secure(domain.RoleAdmin,m.create))
	mux.Handle("GET /api/v1/script-apps/{id}",secure(domain.RoleViewer,m.get))
	mux.Handle("GET /api/v1/script-apps/{id}/logs",secure(domain.RoleViewer,m.logs))
	mux.Handle("POST /api/v1/script-apps/{id}/refresh",secure(domain.RoleViewer,m.refresh))
	mux.Handle("POST /api/v1/script-apps/{id}/{action}",secure(domain.RoleAdmin,m.action))
	mux.Handle("DELETE /api/v1/script-apps/{id}",secure(domain.RoleAdmin,m.delete))
}

func(m *Module)list(w http.ResponseWriter,r *http.Request){items,err:=m.service.List(r.Context());if err!=nil{m.fail(w,err);return};writeData(w,http.StatusOK,items)}
func(m *Module)get(w http.ResponseWriter,r *http.Request){item,err:=m.service.Get(r.Context(),r.PathValue("id"));if err!=nil{m.fail(w,err);return};writeData(w,http.StatusOK,item)}
func(m *Module)logs(w http.ResponseWriter,r *http.Request){item,err:=m.service.Logs(r.Context(),r.PathValue("id"));if err!=nil{m.fail(w,err);return};writeData(w,http.StatusOK,item)}
func(m *Module)refresh(w http.ResponseWriter,r *http.Request){item,err:=m.service.RefreshStatus(r.Context(),r.PathValue("id"));if err!=nil{m.fail(w,err);return};writeData(w,http.StatusOK,item)}
func(m *Module)create(w http.ResponseWriter,r *http.Request){
	var in CreateInput;if err:=decodeJSON(w,r,&in);err!=nil{writeError(w,http.StatusBadRequest,"invalid_request",err.Error());return}
	actor:=actorID(r);item,job,err:=m.service.Create(r.Context(),in,actor);if err!=nil{m.fail(w,err);return}
	meta:=map[string]any{"install_source":item.InstallSource,"install_now":in.InstallNow};if job!=nil{meta["job_id"]=job.ID}
	if err:=m.audit.Record(r.Context(),actor,"script_app.create","script_app",&item.ID,meta,nil);err!=nil{writeError(w,http.StatusInternalServerError,"audit_failed","script application created but audit persistence failed");return}
	status:=http.StatusCreated;if job!=nil{status=http.StatusAccepted}
	writeData(w,status,map[string]any{"app":item,"job":job})
}
func(m *Module)action(w http.ResponseWriter,r *http.Request){
	id,action:=r.PathValue("id"),strings.ToLower(strings.TrimSpace(r.PathValue("action")))
	switch action{case "install","update","uninstall","start","stop","restart":default:writeError(w,http.StatusNotFound,"not_found","unsupported action");return}
	actor:=actorID(r);job,err:=m.service.Action(r.Context(),id,action,actor);if err!=nil{m.fail(w,err);return}
	if err:=m.audit.Record(r.Context(),actor,"script_app."+action,"script_app",&id,map[string]any{"job_id":job.ID},nil);err!=nil{writeError(w,http.StatusInternalServerError,"audit_failed","operation queued but audit persistence failed");return}
	writeData(w,http.StatusAccepted,job)
}
func(m *Module)delete(w http.ResponseWriter,r *http.Request){
	id:=r.PathValue("id");if err:=m.service.Delete(r.Context(),id);err!=nil{m.fail(w,err);return};actor:=actorID(r)
	if err:=m.audit.Record(r.Context(),actor,"script_app.delete","script_app",&id,nil,nil);err!=nil{writeError(w,http.StatusInternalServerError,"audit_failed","record deleted but audit persistence failed");return}
	writeData(w,http.StatusOK,map[string]string{"status":"deleted"})
}
func(m *Module)fail(w http.ResponseWriter,err error){
	switch{case errors.Is(err,ErrNotFound):writeError(w,http.StatusNotFound,"not_found","script application not found")
	case errors.Is(err,ErrInvalidInput):writeError(w,http.StatusBadRequest,"invalid_request",err.Error())
	case strings.Contains(strings.ToLower(err.Error()),"unique constraint"):writeError(w,http.StatusConflict,"conflict","application name already exists")
	default:writeError(w,http.StatusInternalServerError,"internal_error",err.Error())}
}
func actorID(r *http.Request)*string{u,ok:=api.CurrentUser(r.Context());if !ok{return nil};v:=u.ID;return &v}
func decodeJSON(w http.ResponseWriter,r *http.Request,dst any)error{dec:=json.NewDecoder(http.MaxBytesReader(w,r.Body,1<<20));dec.DisallowUnknownFields();if err:=dec.Decode(dst);err!=nil{return errors.New("invalid JSON request")};return nil}
func writeData(w http.ResponseWriter,status int,data any){w.Header().Set("Content-Type","application/json; charset=utf-8");w.WriteHeader(status);_ = json.NewEncoder(w).Encode(map[string]any{"data":data})}
func writeError(w http.ResponseWriter,status int,code,message string){w.Header().Set("Content-Type","application/json; charset=utf-8");w.WriteHeader(status);_ = json.NewEncoder(w).Encode(map[string]any{"error":map[string]string{"code":code,"message":message}})}
