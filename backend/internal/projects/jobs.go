package projects

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	jobpkg "github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/runtimes"
)

const (
	JobClone    = "project.git.clone"
	JobFetch    = "project.git.fetch"
	JobPull     = "project.git.pull"
	JobCheckout = "project.git.checkout"
	JobDeploy   = "project.deploy"
)

type jobLogger interface { Log(ctx context.Context, jobID, level, message string, fields map[string]any) error }

type GitJobHandler struct { typeName string; repo *Repository; git *GitClient; logger jobLogger }
func NewGitJobHandler(typeName string, repo *Repository, git *GitClient, logger jobLogger) *GitJobHandler { return &GitJobHandler{typeName:typeName, repo:repo, git:git, logger:logger} }
func (h *GitJobHandler) Type() string { return h.typeName }
func (h *GitJobHandler) Run(ctx context.Context, job domain.Job) (map[string]any, error) {
	projectID, err := payloadString(job.Payload, "project_id"); if err != nil { return nil, err }
	p, err := h.repo.Get(ctx, projectID); if err != nil { return nil, err }
	cred := CredentialRef(p.CredentialKind,p.ID,"default")
	log := func(message string){ _ = h.logger.Log(ctx,job.ID,"info",message,map[string]any{"project_id":p.ID}) }
	switch h.typeName {
	case JobClone:
		if p.SourceType != SourceGit { return nil, errors.New("clone is only valid for Git projects") }
		if h.git.IsRepository(ctx,p.LocalPath){ return nil,errors.New("destination already contains a Git repository") }
		if err:=os.MkdirAll(filepath.Dir(p.LocalPath),0o750);err!=nil{return nil,err};log("git.clone.start")
		if err:=h.git.Clone(ctx,providers.GitSource{RepositoryURL:p.RepositoryURL,Reference:p.Branch,Destination:p.LocalPath,CredentialRef:cred});err!=nil{_ = h.repo.UpdateStatus(ctx,p.ID,"failed");return nil,err}
	case JobFetch: log("git.fetch.start"); if err:=h.git.Fetch(ctx,p.LocalPath,cred);err!=nil{return nil,err}
	case JobPull: log("git.pull.start"); if err:=h.git.PullWithCredential(ctx,p.LocalPath,cred);err!=nil{return nil,err}
	case JobCheckout: branch,err:=payloadString(job.Payload,"branch");if err!=nil{return nil,err};log("git.checkout.start");if err:=h.git.Checkout(ctx,p.LocalPath,branch);err!=nil{return nil,err}
	default:return nil,fmt.Errorf("unsupported Git job %s",h.typeName)
	}
	state,err:=h.git.State(ctx,p.LocalPath);if err!=nil{return nil,err};if err:=h.repo.UpdateGitState(ctx,p.ID,state.Branch,state.Commit);err!=nil{return nil,err};if h.typeName==JobClone{_ = h.repo.UpdateStatus(ctx,p.ID,"ready")};log("git.operation.success");return map[string]any{"branch":state.Branch,"commit":state.Commit,"dirty":state.Dirty,"ahead":state.Ahead,"behind":state.Behind},nil
}

type DeploymentHandler struct { repo *Repository; git *GitClient; runtimes runtimes.Registry; logger jobLogger }
func NewDeploymentHandler(repo *Repository, git *GitClient, registry runtimes.Registry, logger jobLogger) *DeploymentHandler { return &DeploymentHandler{repo:repo,git:git,runtimes:registry,logger:logger} }
func (h *DeploymentHandler) Type() string { return JobDeploy }
func (h *DeploymentHandler) Run(ctx context.Context, job domain.Job)(result map[string]any,runErr error){
	projectID,err:=payloadString(job.Payload,"project_id");if err!=nil{return nil,err};deploymentID,err:=payloadString(job.Payload,"deployment_id");if err!=nil{return nil,err};p,err:=h.repo.Get(ctx,projectID);if err!=nil{return nil,err};started:=time.Now().UTC();commitBefore:=p.CurrentCommit;if h.git.IsRepository(ctx,p.LocalPath){if revision,revErr:=h.git.Revision(ctx,p.LocalPath);revErr==nil{commitBefore=revision}}
	if err:=h.repo.StartDeployment(ctx,deploymentID,DeploymentPreparing,commitBefore,started);err!=nil{return nil,err};_ = h.repo.UpdateStatus(ctx,p.ID,"deploying");currentStage:=DeploymentPreparing;commitAfter:=commitBefore
	defer func(){if runErr==nil{return};finished:=time.Now().UTC();_ = h.repo.FinishDeployment(context.Background(),deploymentID,DeploymentFailed,DeploymentFailed,commitAfter,runErr.Error(),finished,finished.Sub(started));_ = h.repo.UpdateStatus(context.Background(),p.ID,"failed");_ = h.logger.Log(context.Background(),job.ID,"error","deployment.failed",map[string]any{"stage":currentStage,"error":runErr.Error()})}()
	setStage:=func(next string)error{if !validDeploymentTransition(currentStage,next){return fmt.Errorf("invalid deployment transition %s -> %s",currentStage,next)};if err:=h.repo.SetDeploymentStage(ctx,deploymentID,next);err!=nil{return err};currentStage=next;_ = h.logger.Log(ctx,job.ID,"info","deployment.stage",map[string]any{"stage":next});return nil}
	if p.DeploymentMode=="docker"{return nil,errors.New("provider unavailable: docker")};workDir,err:=SafeWorkingDirectory(p.LocalPath,p.WorkingDirectory);if err!=nil{return nil,err};if p.Runtime==""{return nil,errors.New("provider unavailable: runtime is not configured")};runtime,ok:=h.runtimes.Get(p.Runtime);if !ok{return nil,fmt.Errorf("provider unavailable: runtime %s",p.Runtime)};validation,err:=runtime.Validate(ctx,runtimeContext(p,workDir));if err!=nil{return nil,fmt.Errorf("runtime validation: %w",err)};if !validation.Valid{return nil,fmt.Errorf("runtime validation failed: %v",validation.Errors)}
	if err:=setStage(DeploymentUpdatingSource);err!=nil{return nil,err};if p.SourceType==SourceGit{if !h.git.IsRepository(ctx,p.LocalPath){return nil,errors.New("provider unavailable: Git repository is not cloned")};if err:=h.git.PullWithCredential(ctx,p.LocalPath,CredentialRef(p.CredentialKind,p.ID,"default"));err!=nil{return nil,err}};if h.git.IsRepository(ctx,p.LocalPath){commitAfter,err=h.git.Revision(ctx,p.LocalPath);if err!=nil{return nil,err};if state,stateErr:=h.git.State(ctx,p.LocalPath);stateErr==nil{_ = h.repo.UpdateGitState(ctx,p.ID,state.Branch,state.Commit)}}
	if err:=setStage(DeploymentDependencies);err!=nil{return nil,err};if err:=runtime.InstallDependencies(ctx,runtimeContext(p,workDir));err!=nil{return nil,fmt.Errorf("install dependencies: %w",err)};if err:=setStage(DeploymentBuilding);err!=nil{return nil,err};if err:=runtime.Build(ctx,runtimeContext(p,workDir));err!=nil{return nil,fmt.Errorf("build: %w",err)};if err:=setStage(DeploymentStarting);err!=nil{return nil,err};if err:=runtime.Start(ctx,runtimeContext(p,workDir));err!=nil{return nil,fmt.Errorf("start: %w",err)};if err:=setStage(DeploymentHealthcheck);err!=nil{return nil,err};health,err:=runtime.HealthCheck(ctx,runtimeContext(p,workDir));if err!=nil{return nil,fmt.Errorf("healthcheck: %w",err)};if !health.Healthy{return nil,fmt.Errorf("healthcheck failed: %s",health.Message)};if err:=setStage(DeploymentSuccess);err!=nil{return nil,err};finished:=time.Now().UTC();if err:=h.repo.FinishDeployment(ctx,deploymentID,DeploymentSuccess,DeploymentSuccess,commitAfter,"",finished,finished.Sub(started));err!=nil{return nil,err};_ = h.repo.UpdateStatus(ctx,p.ID,"running");return map[string]any{"deployment_id":deploymentID,"commit_before":commitBefore,"commit_after":commitAfter,"duration_ms":finished.Sub(started).Milliseconds()},nil
}
func runtimeContext(p Project,workDir string)runtimes.ProjectContext{return runtimes.ProjectContext{ProjectID:p.ID,ProjectName:p.Name,WorkDir:workDir,Config:map[string]any{"build_command":p.BuildCommand,"start_command":p.StartCommand,"healthcheck":p.Healthcheck,"auto_start":p.AutoStart,"deployment_mode":p.DeploymentMode}}}
func payloadString(payload map[string]any,key string)(string,error){value,ok:=payload[key].(string);if !ok||value==""{return "",fmt.Errorf("job payload missing %s",key)};return value,nil}
func validDeploymentTransition(from,to string)bool{next:=map[string]string{DeploymentQueued:DeploymentPreparing,DeploymentPreparing:DeploymentUpdatingSource,DeploymentUpdatingSource:DeploymentDependencies,DeploymentDependencies:DeploymentBuilding,DeploymentBuilding:DeploymentStarting,DeploymentStarting:DeploymentHealthcheck,DeploymentHealthcheck:DeploymentSuccess};return next[from]==to}
var _ jobpkg.Handler=(*GitJobHandler)(nil)
var _ jobpkg.Handler=(*DeploymentHandler)(nil)
