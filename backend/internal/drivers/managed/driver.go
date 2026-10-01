package managed

import (
	"context"
	"fmt"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/applications"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/containerspec"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/drivers/driverutil"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/runtimes"
)

type engine interface {
	Available(context.Context) error
	ManagedImageExists(context.Context,string)(bool,error)
	BuildManaged(context.Context,containerspec.DeploymentSpec) error
	ReplaceManagedPorts(context.Context,containerspec.DeploymentSpec,[]providers.PublishedPort) error
	Inspect(context.Context,string)(providers.ContainerInfo,error)
	Start(context.Context,string) error
	Stop(context.Context,string) error
	Restart(context.Context,string) error
	Remove(context.Context,string) error
}

type networkProvider interface {
	EnsureNetwork(context.Context,string) error
}

type Driver struct {
	engine engine
	runtimes runtimes.Registry
	ports applications.PortAllocator
	networks networkProvider
	sharedNetwork string
}

func New(engine engine, registry runtimes.Registry, ports applications.PortAllocator, networks networkProvider, sharedNetwork string)*Driver{
	return &Driver{engine:engine,runtimes:registry,ports:ports,networks:networks,sharedNetwork:sharedNetwork}
}
func(d *Driver)Name()string{return "managed"}

func(d *Driver)Detect(ctx context.Context,request applications.DetectRequest)(applications.DetectionResult,error){
	if request.WorkDir==""{return applications.DetectionResult{},applications.ErrConfigurationRequired}
	bestRuntime,bestVersion,bestConfidence:="","",-1
	for _,name:=range d.runtimes.List(){
		runtime,ok:=d.runtimes.Get(name);if !ok{continue}
		detection,err:=runtime.Detect(ctx,runtimes.ProjectContext{ProjectID:"detect",ProjectName:"detect",WorkDir:request.WorkDir,Config:request.Configuration})
		if err!=nil||!detection.Detected{continue}
		confidence:=confidenceValue(detection.Metadata)
		if confidence>bestConfidence{bestRuntime,bestVersion,bestConfidence=detection.Runtime,detection.Version,confidence}
	}
	if bestRuntime==""{
		return applications.DetectionResult{Driver:d.Name(),Confidence:"none",RequiresConfiguration:true},applications.ErrConfigurationRequired
	}
	return applications.DetectionResult{
		Driver:d.Name(),Confidence:confidenceLabel(bestConfidence),Runtime:bestRuntime,Version:bestVersion,
		Services:[]applications.ServiceDetection{{Name:"web",SuggestedRole:"web",Primary:true,Confidence:"high",Reason:"single managed runtime workload"}},
		Endpoints:[]applications.EndpointDetection{{Service:"web",Protocol:"http",ContainerPort:8080,Primary:true,Confidence:"medium",Reason:"managed runtime default; final port is derived from generated container specification"}},
		Reasons:[]string{"runtime manifest/files detected"},
	},nil
}

func(d *Driver)Plan(ctx context.Context,request applications.PlanRequest)(applications.DeploymentPlan,error){
	runtimeName:=strings.TrimSpace(request.Detection.Runtime)
	if runtimeName==""{
		detection,err:=d.Detect(ctx,applications.DetectRequest{SourceType:request.Application.SourceType,Source:request.Source,WorkDir:request.WorkDir,Configuration:request.Configuration})
		if err!=nil{return applications.DeploymentPlan{},err}
		runtimeName=detection.Runtime
		request.Detection=detection
	}
	version:=strings.TrimSpace(request.Detection.Version)
	spec,err:=containerspec.GenerateManaged(request.Application.ID,request.WorkDir,runtimeName,version,nil,request.SourceRevision,1)
	if err!=nil{return applications.DeploymentPlan{},err}
	role,primary:="web",true
	if manifest,err:=applications.LoadManifest(request.WorkDir);err!=nil{return applications.DeploymentPlan{},err}else if manifest!=nil{
		for _,item:=range manifest.Workloads{if item.Role!=""{role=item.Role};if item.Primary{primary=true}}
	}
	endpoint:=applications.PlannedEndpoint{Name:"web",Workload:"web",Protocol:"http",ContainerPort:spec.ContainerPort,Public:true,Primary:true,HealthPath:"/"}
	if manifest,err:=applications.LoadManifest(request.WorkDir);err==nil&&manifest!=nil&&len(manifest.Endpoints)>0{
		for name,item:=range manifest.Endpoints{
			endpoint=applications.PlannedEndpoint{Name:name,Workload:item.Workload,Protocol:item.Protocol,ContainerPort:item.ContainerPort,Public:item.Public,Primary:item.Primary,HealthPath:item.HealthPath}
			if endpoint.Protocol==""{endpoint.Protocol="http"}
			break
		}
	}
	return applications.DeploymentPlan{
		Version:1,ApplicationID:request.Application.ID,Driver:d.Name(),SourceRevision:request.SourceRevision,
		Runtime:&applications.Runtime{ApplicationID:request.Application.ID,Name:runtimeName,Version:spec.Version,Metadata:map[string]any{"adapter":"managed"}},
		Workloads:[]applications.PlannedWorkload{{Name:"web",Role:role,Primary:primary,Runtime:runtimeName,Image:spec.Image}},
		Endpoints:[]applications.PlannedEndpoint{endpoint},
		Networks:nonEmpty(d.sharedNetwork),Healthchecks:[]applications.HealthCheckPlan{{Workload:"web",Type:"http",Path:endpoint.HealthPath,Port:endpoint.ContainerPort}},
		Metadata:map[string]any{"fingerprint":spec.Fingerprint},
	},nil
}

func(d *Driver)Deploy(ctx context.Context,request applications.ExecutionRequest,plan applications.DeploymentPlan)(applications.DeploymentResult,error){
	if err:=d.engine.Available(ctx);err!=nil{return applications.DeploymentResult{},fmt.Errorf("%w: docker: %v",applications.ErrProviderUnavailable,err)}
	if len(plan.Workloads)!=1||len(plan.Endpoints)!=1{return applications.DeploymentResult{},fmt.Errorf("%w: managed driver requires one workload and endpoint",applications.ErrInvalidInput)}
	workloadPlan,endpointPlan:=plan.Workloads[0],plan.Endpoints[0]
	endpoint,err:=driverutil.Endpoint(request,endpointPlan.Name);if err!=nil{return applications.DeploymentResult{},err}
	lease,err:=d.ports.Reserve(ctx,request.Application.ID,endpoint.ID,"application-http",preferred(endpointPlan.HostPort))
	if err!=nil{return applications.DeploymentResult{},&applications.OperationError{Stage:applications.StageNetwork,Driver:d.Name(),Workload:workloadPlan.Name,Operation:"reserve_port",Reason:err.Error(),Action:"change the requested host port or release the collision"}}
	spec,err:=containerspec.GenerateManaged(request.Application.ID,request.WorkDir,plan.Runtime.Name,plan.Runtime.Version,nil,plan.SourceRevision,lease.Port)
	if err!=nil{_ = d.ports.Release(context.Background(),request.Application.ID,endpoint.ID,lease.Port);return applications.DeploymentResult{},err}
	spec.Labels=driverutil.MergeLabels(spec.Labels,driverutil.Labels(request.Application,request.Deployment,workloadPlan.Name))
	if d.sharedNetwork!=""{
		if d.networks==nil{_ = d.ports.Release(context.Background(),request.Application.ID,endpoint.ID,lease.Port);return applications.DeploymentResult{},fmt.Errorf("%w: network provider",applications.ErrProviderUnavailable)}
		if err:=d.networks.EnsureNetwork(ctx,d.sharedNetwork);err!=nil{_ = d.ports.Release(context.Background(),request.Application.ID,endpoint.ID,lease.Port);return applications.DeploymentResult{},err}
		if !contains(spec.Networks,d.sharedNetwork){spec.Networks=append(spec.Networks,d.sharedNetwork)}
	}
	exists,err:=d.engine.ManagedImageExists(ctx,spec.Image)
	if err!=nil{_ = d.ports.Release(context.Background(),request.Application.ID,endpoint.ID,lease.Port);return applications.DeploymentResult{},err}
	if !exists{
		if err:=d.engine.BuildManaged(ctx,spec);err!=nil{_ = d.ports.Release(context.Background(),request.Application.ID,endpoint.ID,lease.Port);return applications.DeploymentResult{},&applications.OperationError{Stage:applications.StageBuildOrPull,Driver:d.Name(),Workload:workloadPlan.Name,Operation:"build_image",Reason:err.Error(),Action:"inspect build logs and runtime dependencies"}}
	}
	if err:=d.engine.ReplaceManagedPorts(ctx,spec,nil);err!=nil{
		_ = d.ports.Release(context.Background(),request.Application.ID,endpoint.ID,lease.Port)
		return applications.DeploymentResult{},&applications.OperationError{Stage:applications.StageHealthcheck,Driver:d.Name(),Workload:workloadPlan.Name,Operation:"replace_container",Reason:err.Error(),Action:"previous container was restored when possible"}
	}
	if endpoint.HostPort!=nil&&*endpoint.HostPort!=lease.Port{_ = d.ports.Release(context.Background(),request.Application.ID,endpoint.ID,*endpoint.HostPort)}
	info,err:=d.engine.Inspect(ctx,spec.ContainerName);if err!=nil{return applications.DeploymentResult{},err}
	observed,health:=driverutil.Observed(info)
	return applications.DeploymentResult{
		Resources:map[string]applications.ResourceState{workloadPlan.Name:{ResourceID:spec.ContainerName,Image:spec.Image,ObservedState:observed,HealthState:health}},
		EndpointPorts:map[string]int{endpointPlan.Name:lease.Port},
	},nil
}

func(d *Driver)Inspect(ctx context.Context,request applications.InspectRequest)([]applications.ObservedWorkload,error){
	out:=make([]applications.ObservedWorkload,0,len(request.Workloads))
	for _,workload:=range request.Workloads{
		id:=workload.DriverResourceID
		if id==""{out=append(out,applications.ObservedWorkload{Name:workload.Name,ObservedState:applications.ObservedMissing,HealthState:applications.HealthUnknown});continue}
		info,err:=d.engine.Inspect(ctx,id)
		if err!=nil{
			if driverutil.MissingError(err){out=append(out,applications.ObservedWorkload{Name:workload.Name,ResourceID:id,ObservedState:applications.ObservedMissing,HealthState:applications.HealthUnknown});continue}
			return nil,err
		}
		observed,health:=driverutil.Observed(info)
		out=append(out,applications.ObservedWorkload{Name:workload.Name,ResourceID:id,Image:info.Image,ObservedState:observed,HealthState:health})
	}
	return out,nil
}

func(d *Driver)Start(ctx context.Context,request applications.InspectRequest)error{return d.each(ctx,request,d.engine.Start)}
func(d *Driver)Stop(ctx context.Context,request applications.InspectRequest)error{return d.each(ctx,request,d.engine.Stop)}
func(d *Driver)Restart(ctx context.Context,request applications.InspectRequest)error{return d.each(ctx,request,d.engine.Restart)}
func(d *Driver)Remove(ctx context.Context,request applications.InspectRequest)error{
	return d.each(ctx,request,func(ctx context.Context,id string)error{_ = d.engine.Stop(ctx,id);return d.engine.Remove(ctx,id)})
}
func(d *Driver)each(ctx context.Context,request applications.InspectRequest,action func(context.Context,string)error)error{
	for _,workload:=range request.Workloads{
		if workload.DriverResourceID==""{continue}
		if err:=action(ctx,workload.DriverResourceID);err!=nil&&!driverutil.MissingError(err){return err}
	}
	return nil
}

func confidenceValue(metadata map[string]any)int{
	if metadata==nil{return 0}
	switch value:=metadata["confidence"].(type){case int:return value;case float64:return int(value);case string:if value=="high"{return 100};if value=="medium"{return 50}}
	return 0
}
func confidenceLabel(value int)string{if value>=80{return "high"};if value>=40{return "medium"};return "low"}
func preferred(port int)*int{if port<=0{return nil};return &port}
func contains(values []string,target string)bool{for _,value:=range values{if value==target{return true}};return false}
func nonEmpty(value string)[]string{if strings.TrimSpace(value)==""{return nil};return []string{value}}
