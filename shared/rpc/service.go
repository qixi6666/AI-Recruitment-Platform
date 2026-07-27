package rpc

import (
	"context"

	"google.golang.org/grpc"
)

const (
	authServiceName                 = "recruitment.AuthService"
	jobServiceName                  = "recruitment.JobService"
	candidateServiceName            = "recruitment.CandidateService"
	applicationServiceName          = "recruitment.ApplicationService"
	resumeRecommendationServiceName = "recruitment.ResumeRecommendationService"
	llmGatewayServiceName           = "recruitment.LLMGatewayService"
)

type AuthServiceClient interface {
	Register(ctx context.Context, in *RegisterRequest, opts ...grpc.CallOption) (*RegisterResponse, error)
	Login(ctx context.Context, in *LoginRequest, opts ...grpc.CallOption) (*LoginResponse, error)
}

type authServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewAuthServiceClient(cc grpc.ClientConnInterface) AuthServiceClient {
	return &authServiceClient{cc: cc}
}

func (c *authServiceClient) Register(ctx context.Context, in *RegisterRequest, opts ...grpc.CallOption) (*RegisterResponse, error) {
	out := new(RegisterResponse)
	err := c.cc.Invoke(ctx, "/recruitment.AuthService/Register", in, out, opts...)
	return out, err
}

func (c *authServiceClient) Login(ctx context.Context, in *LoginRequest, opts ...grpc.CallOption) (*LoginResponse, error) {
	out := new(LoginResponse)
	err := c.cc.Invoke(ctx, "/recruitment.AuthService/Login", in, out, opts...)
	return out, err
}

type JobServiceClient interface {
	ListJobs(ctx context.Context, in *ListJobsRequest, opts ...grpc.CallOption) (*ListJobsResponse, error)
	CreateJob(ctx context.Context, in *CreateJobRequest, opts ...grpc.CallOption) (*JobResponse, error)
	UpdateJob(ctx context.Context, in *UpdateJobRequest, opts ...grpc.CallOption) (*JobResponse, error)
	OfflineJob(ctx context.Context, in *OfflineJobRequest, opts ...grpc.CallOption) (*JobResponse, error)
}

type jobServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewJobServiceClient(cc grpc.ClientConnInterface) JobServiceClient {
	return &jobServiceClient{cc: cc}
}

func (c *jobServiceClient) ListJobs(ctx context.Context, in *ListJobsRequest, opts ...grpc.CallOption) (*ListJobsResponse, error) {
	out := new(ListJobsResponse)
	err := c.cc.Invoke(ctx, "/recruitment.JobService/ListJobs", in, out, opts...)
	return out, err
}

func (c *jobServiceClient) CreateJob(ctx context.Context, in *CreateJobRequest, opts ...grpc.CallOption) (*JobResponse, error) {
	out := new(JobResponse)
	err := c.cc.Invoke(ctx, "/recruitment.JobService/CreateJob", in, out, opts...)
	return out, err
}

func (c *jobServiceClient) UpdateJob(ctx context.Context, in *UpdateJobRequest, opts ...grpc.CallOption) (*JobResponse, error) {
	out := new(JobResponse)
	err := c.cc.Invoke(ctx, "/recruitment.JobService/UpdateJob", in, out, opts...)
	return out, err
}

func (c *jobServiceClient) OfflineJob(ctx context.Context, in *OfflineJobRequest, opts ...grpc.CallOption) (*JobResponse, error) {
	out := new(JobResponse)
	err := c.cc.Invoke(ctx, "/recruitment.JobService/OfflineJob", in, out, opts...)
	return out, err
}

type CandidateServiceClient interface {
	GetProfile(ctx context.Context, in *GetProfileRequest, opts ...grpc.CallOption) (*ProfileResponse, error)
	UpsertProfile(ctx context.Context, in *UpsertProfileRequest, opts ...grpc.CallOption) (*ProfileResponse, error)
	SaveResume(ctx context.Context, in *SaveResumeRequest, opts ...grpc.CallOption) (*ResumeResponse, error)
	GetResume(ctx context.Context, in *GetResumeRequest, opts ...grpc.CallOption) (*ResumeResponse, error)
}

type candidateServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewCandidateServiceClient(cc grpc.ClientConnInterface) CandidateServiceClient {
	return &candidateServiceClient{cc: cc}
}

func (c *candidateServiceClient) GetProfile(ctx context.Context, in *GetProfileRequest, opts ...grpc.CallOption) (*ProfileResponse, error) {
	out := new(ProfileResponse)
	err := c.cc.Invoke(ctx, "/recruitment.CandidateService/GetProfile", in, out, opts...)
	return out, err
}

func (c *candidateServiceClient) UpsertProfile(ctx context.Context, in *UpsertProfileRequest, opts ...grpc.CallOption) (*ProfileResponse, error) {
	out := new(ProfileResponse)
	err := c.cc.Invoke(ctx, "/recruitment.CandidateService/UpsertProfile", in, out, opts...)
	return out, err
}

func (c *candidateServiceClient) SaveResume(ctx context.Context, in *SaveResumeRequest, opts ...grpc.CallOption) (*ResumeResponse, error) {
	out := new(ResumeResponse)
	err := c.cc.Invoke(ctx, "/recruitment.CandidateService/SaveResume", in, out, opts...)
	return out, err
}

func (c *candidateServiceClient) GetResume(ctx context.Context, in *GetResumeRequest, opts ...grpc.CallOption) (*ResumeResponse, error) {
	out := new(ResumeResponse)
	err := c.cc.Invoke(ctx, "/recruitment.CandidateService/GetResume", in, out, opts...)
	return out, err
}

type ApplicationServiceClient interface {
	ApplyJob(ctx context.Context, in *ApplyJobRequest, opts ...grpc.CallOption) (*ApplyJobResponse, error)
	ListApplications(ctx context.Context, in *ListApplicationsRequest, opts ...grpc.CallOption) (*ListApplicationsResponse, error)
}

type applicationServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewApplicationServiceClient(cc grpc.ClientConnInterface) ApplicationServiceClient {
	return &applicationServiceClient{cc: cc}
}

func (c *applicationServiceClient) ApplyJob(ctx context.Context, in *ApplyJobRequest, opts ...grpc.CallOption) (*ApplyJobResponse, error) {
	out := new(ApplyJobResponse)
	err := c.cc.Invoke(ctx, "/recruitment.ApplicationService/ApplyJob", in, out, opts...)
	return out, err
}

func (c *applicationServiceClient) ListApplications(ctx context.Context, in *ListApplicationsRequest, opts ...grpc.CallOption) (*ListApplicationsResponse, error) {
	out := new(ListApplicationsResponse)
	err := c.cc.Invoke(ctx, "/recruitment.ApplicationService/ListApplications", in, out, opts...)
	return out, err
}

type ResumeRecommendationServiceClient interface {
	RecommendResumes(ctx context.Context, in *ResumeRecommendationRequest, opts ...grpc.CallOption) (*ResumeRecommendationResponse, error)
	CreateResumeRecommendationTask(ctx context.Context, in *ResumeRecommendationRequest, opts ...grpc.CallOption) (*ResumeRecommendationTaskResponse, error)
	RecommendResumesStream(ctx context.Context, in *ResumeRecommendationRequest, opts ...grpc.CallOption) (ResumeRecommendationService_RecommendResumesStreamClient, error)
	WatchResumeRecommendationTask(ctx context.Context, in *ResumeRecommendationTaskWatchRequest, opts ...grpc.CallOption) (ResumeRecommendationService_WatchResumeRecommendationTaskClient, error)
}

type LLMGatewayServiceClient interface {
	ListModels(ctx context.Context, in *ListLLMModelsRequest, opts ...grpc.CallOption) (*ListLLMModelsResponse, error)
	Chat(ctx context.Context, in *LLMChatRequest, opts ...grpc.CallOption) (*LLMChatResponse, error)
	Evaluate(ctx context.Context, in *LLMEvaluateRequest, opts ...grpc.CallOption) (*LLMEvaluateResponse, error)
}

type llmGatewayServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewLLMGatewayServiceClient(cc grpc.ClientConnInterface) LLMGatewayServiceClient {
	return &llmGatewayServiceClient{cc: cc}
}

func (c *llmGatewayServiceClient) ListModels(ctx context.Context, in *ListLLMModelsRequest, opts ...grpc.CallOption) (*ListLLMModelsResponse, error) {
	out := new(ListLLMModelsResponse)
	err := c.cc.Invoke(ctx, "/recruitment.LLMGatewayService/ListModels", in, out, opts...)
	return out, err
}

func (c *llmGatewayServiceClient) Chat(ctx context.Context, in *LLMChatRequest, opts ...grpc.CallOption) (*LLMChatResponse, error) {
	out := new(LLMChatResponse)
	err := c.cc.Invoke(ctx, "/recruitment.LLMGatewayService/Chat", in, out, opts...)
	return out, err
}

func (c *llmGatewayServiceClient) Evaluate(ctx context.Context, in *LLMEvaluateRequest, opts ...grpc.CallOption) (*LLMEvaluateResponse, error) {
	out := new(LLMEvaluateResponse)
	err := c.cc.Invoke(ctx, "/recruitment.LLMGatewayService/Evaluate", in, out, opts...)
	return out, err
}

type resumeRecommendationServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewResumeRecommendationServiceClient(cc grpc.ClientConnInterface) ResumeRecommendationServiceClient {
	return &resumeRecommendationServiceClient{cc: cc}
}

func (c *resumeRecommendationServiceClient) RecommendResumes(ctx context.Context, in *ResumeRecommendationRequest, opts ...grpc.CallOption) (*ResumeRecommendationResponse, error) {
	out := new(ResumeRecommendationResponse)
	err := c.cc.Invoke(ctx, "/recruitment.ResumeRecommendationService/RecommendResumes", in, out, opts...)
	return out, err
}

func (c *resumeRecommendationServiceClient) CreateResumeRecommendationTask(ctx context.Context, in *ResumeRecommendationRequest, opts ...grpc.CallOption) (*ResumeRecommendationTaskResponse, error) {
	out := new(ResumeRecommendationTaskResponse)
	err := c.cc.Invoke(ctx, "/recruitment.ResumeRecommendationService/CreateResumeRecommendationTask", in, out, opts...)
	return out, err
}

func (c *resumeRecommendationServiceClient) RecommendResumesStream(ctx context.Context, in *ResumeRecommendationRequest, opts ...grpc.CallOption) (ResumeRecommendationService_RecommendResumesStreamClient, error) {
	stream, err := c.cc.NewStream(ctx, &ResumeRecommendationService_ServiceDesc.Streams[0], "/recruitment.ResumeRecommendationService/RecommendResumesStream", opts...)
	if err != nil {
		return nil, err
	}
	x := &resumeRecommendationServiceRecommendResumesStreamClient{ClientStream: stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

func (c *resumeRecommendationServiceClient) WatchResumeRecommendationTask(ctx context.Context, in *ResumeRecommendationTaskWatchRequest, opts ...grpc.CallOption) (ResumeRecommendationService_WatchResumeRecommendationTaskClient, error) {
	stream, err := c.cc.NewStream(ctx, &ResumeRecommendationService_ServiceDesc.Streams[1], "/recruitment.ResumeRecommendationService/WatchResumeRecommendationTask", opts...)
	if err != nil {
		return nil, err
	}
	x := &resumeRecommendationServiceWatchResumeRecommendationTaskClient{ClientStream: stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type ResumeRecommendationService_RecommendResumesStreamClient interface {
	Recv() (*ResumeRecommendationStreamChunk, error)
	grpc.ClientStream
}

type resumeRecommendationServiceRecommendResumesStreamClient struct {
	grpc.ClientStream
}

func (x *resumeRecommendationServiceRecommendResumesStreamClient) Recv() (*ResumeRecommendationStreamChunk, error) {
	m := new(ResumeRecommendationStreamChunk)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

type ResumeRecommendationService_WatchResumeRecommendationTaskClient interface {
	Recv() (*ResumeRecommendationStreamChunk, error)
	grpc.ClientStream
}

type resumeRecommendationServiceWatchResumeRecommendationTaskClient struct {
	grpc.ClientStream
}

func (x *resumeRecommendationServiceWatchResumeRecommendationTaskClient) Recv() (*ResumeRecommendationStreamChunk, error) {
	m := new(ResumeRecommendationStreamChunk)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

type AuthServiceServer interface {
	Register(context.Context, *RegisterRequest) (*RegisterResponse, error)
	Login(context.Context, *LoginRequest) (*LoginResponse, error)
}

type JobServiceServer interface {
	ListJobs(context.Context, *ListJobsRequest) (*ListJobsResponse, error)
	CreateJob(context.Context, *CreateJobRequest) (*JobResponse, error)
	UpdateJob(context.Context, *UpdateJobRequest) (*JobResponse, error)
	OfflineJob(context.Context, *OfflineJobRequest) (*JobResponse, error)
}

type CandidateServiceServer interface {
	GetProfile(context.Context, *GetProfileRequest) (*ProfileResponse, error)
	UpsertProfile(context.Context, *UpsertProfileRequest) (*ProfileResponse, error)
	SaveResume(context.Context, *SaveResumeRequest) (*ResumeResponse, error)
	GetResume(context.Context, *GetResumeRequest) (*ResumeResponse, error)
}

type ApplicationServiceServer interface {
	ApplyJob(context.Context, *ApplyJobRequest) (*ApplyJobResponse, error)
	ListApplications(context.Context, *ListApplicationsRequest) (*ListApplicationsResponse, error)
}

type ResumeRecommendationServiceServer interface {
	RecommendResumes(context.Context, *ResumeRecommendationRequest) (*ResumeRecommendationResponse, error)
	CreateResumeRecommendationTask(context.Context, *ResumeRecommendationRequest) (*ResumeRecommendationTaskResponse, error)
	RecommendResumesStream(*ResumeRecommendationRequest, ResumeRecommendationService_RecommendResumesStreamServer) error
	WatchResumeRecommendationTask(*ResumeRecommendationTaskWatchRequest, ResumeRecommendationService_WatchResumeRecommendationTaskServer) error
}

type LLMGatewayServiceServer interface {
	ListModels(context.Context, *ListLLMModelsRequest) (*ListLLMModelsResponse, error)
	Chat(context.Context, *LLMChatRequest) (*LLMChatResponse, error)
	Evaluate(context.Context, *LLMEvaluateRequest) (*LLMEvaluateResponse, error)
}

func RegisterAuthServiceServer(s grpc.ServiceRegistrar, srv AuthServiceServer) {
	s.RegisterService(&AuthService_ServiceDesc, srv)
}

func RegisterJobServiceServer(s grpc.ServiceRegistrar, srv JobServiceServer) {
	s.RegisterService(&JobService_ServiceDesc, srv)
}

func RegisterCandidateServiceServer(s grpc.ServiceRegistrar, srv CandidateServiceServer) {
	s.RegisterService(&CandidateService_ServiceDesc, srv)
}

func RegisterApplicationServiceServer(s grpc.ServiceRegistrar, srv ApplicationServiceServer) {
	s.RegisterService(&ApplicationService_ServiceDesc, srv)
}

func RegisterResumeRecommendationServiceServer(s grpc.ServiceRegistrar, srv ResumeRecommendationServiceServer) {
	s.RegisterService(&ResumeRecommendationService_ServiceDesc, srv)
}

func RegisterLLMGatewayServiceServer(s grpc.ServiceRegistrar, srv LLMGatewayServiceServer) {
	s.RegisterService(&LLMGatewayService_ServiceDesc, srv)
}

func unaryHandler[S any, Req any, Resp any](srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor, serviceName string, method string, fn func(S, context.Context, *Req) (*Resp, error)) (any, error) {
	in := new(Req)
	if err := dec(in); err != nil {
		return nil, err
	}
	typed := srv.(S)
	if interceptor == nil {
		return fn(typed, ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/" + serviceName + "/" + method}
	handler := func(ctx context.Context, req any) (any, error) {
		return fn(typed, ctx, req.(*Req))
	}
	return interceptor(ctx, in, info, handler)
}

func resumeRecommendationServiceRecommendResumesStreamHandler(srv any, stream grpc.ServerStream) error {
	m := new(ResumeRecommendationRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(ResumeRecommendationServiceServer).RecommendResumesStream(m, &resumeRecommendationServiceRecommendResumesStreamServer{ServerStream: stream})
}

func resumeRecommendationServiceWatchResumeRecommendationTaskHandler(srv any, stream grpc.ServerStream) error {
	m := new(ResumeRecommendationTaskWatchRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(ResumeRecommendationServiceServer).WatchResumeRecommendationTask(m, &resumeRecommendationServiceWatchResumeRecommendationTaskServer{ServerStream: stream})
}

type ResumeRecommendationService_RecommendResumesStreamServer interface {
	Send(*ResumeRecommendationStreamChunk) error
	grpc.ServerStream
}

type resumeRecommendationServiceRecommendResumesStreamServer struct {
	grpc.ServerStream
}

func (x *resumeRecommendationServiceRecommendResumesStreamServer) Send(m *ResumeRecommendationStreamChunk) error {
	return x.ServerStream.SendMsg(m)
}

type ResumeRecommendationService_WatchResumeRecommendationTaskServer interface {
	Send(*ResumeRecommendationStreamChunk) error
	grpc.ServerStream
}

type resumeRecommendationServiceWatchResumeRecommendationTaskServer struct {
	grpc.ServerStream
}

func (x *resumeRecommendationServiceWatchResumeRecommendationTaskServer) Send(m *ResumeRecommendationStreamChunk) error {
	return x.ServerStream.SendMsg(m)
}

var AuthService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: authServiceName,
	HandlerType: (*AuthServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "Register", Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			return unaryHandler[AuthServiceServer, RegisterRequest, RegisterResponse](srv, ctx, dec, interceptor, authServiceName, "Register", AuthServiceServer.Register)
		}},
		{MethodName: "Login", Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			return unaryHandler[AuthServiceServer, LoginRequest, LoginResponse](srv, ctx, dec, interceptor, authServiceName, "Login", AuthServiceServer.Login)
		}},
	},
	Metadata: "recruitment.proto",
}

var JobService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: jobServiceName,
	HandlerType: (*JobServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "ListJobs", Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			return unaryHandler[JobServiceServer, ListJobsRequest, ListJobsResponse](srv, ctx, dec, interceptor, jobServiceName, "ListJobs", JobServiceServer.ListJobs)
		}},
		{MethodName: "CreateJob", Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			return unaryHandler[JobServiceServer, CreateJobRequest, JobResponse](srv, ctx, dec, interceptor, jobServiceName, "CreateJob", JobServiceServer.CreateJob)
		}},
		{MethodName: "UpdateJob", Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			return unaryHandler[JobServiceServer, UpdateJobRequest, JobResponse](srv, ctx, dec, interceptor, jobServiceName, "UpdateJob", JobServiceServer.UpdateJob)
		}},
		{MethodName: "OfflineJob", Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			return unaryHandler[JobServiceServer, OfflineJobRequest, JobResponse](srv, ctx, dec, interceptor, jobServiceName, "OfflineJob", JobServiceServer.OfflineJob)
		}},
	},
	Metadata: "recruitment.proto",
}

var CandidateService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: candidateServiceName,
	HandlerType: (*CandidateServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "GetProfile", Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			return unaryHandler[CandidateServiceServer, GetProfileRequest, ProfileResponse](srv, ctx, dec, interceptor, candidateServiceName, "GetProfile", CandidateServiceServer.GetProfile)
		}},
		{MethodName: "UpsertProfile", Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			return unaryHandler[CandidateServiceServer, UpsertProfileRequest, ProfileResponse](srv, ctx, dec, interceptor, candidateServiceName, "UpsertProfile", CandidateServiceServer.UpsertProfile)
		}},
		{MethodName: "SaveResume", Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			return unaryHandler[CandidateServiceServer, SaveResumeRequest, ResumeResponse](srv, ctx, dec, interceptor, candidateServiceName, "SaveResume", CandidateServiceServer.SaveResume)
		}},
		{MethodName: "GetResume", Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			return unaryHandler[CandidateServiceServer, GetResumeRequest, ResumeResponse](srv, ctx, dec, interceptor, candidateServiceName, "GetResume", CandidateServiceServer.GetResume)
		}},
	},
	Metadata: "recruitment.proto",
}

var ApplicationService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: applicationServiceName,
	HandlerType: (*ApplicationServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "ApplyJob", Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			return unaryHandler[ApplicationServiceServer, ApplyJobRequest, ApplyJobResponse](srv, ctx, dec, interceptor, applicationServiceName, "ApplyJob", ApplicationServiceServer.ApplyJob)
		}},
		{MethodName: "ListApplications", Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			return unaryHandler[ApplicationServiceServer, ListApplicationsRequest, ListApplicationsResponse](srv, ctx, dec, interceptor, applicationServiceName, "ListApplications", ApplicationServiceServer.ListApplications)
		}},
	},
	Metadata: "recruitment.proto",
}

var ResumeRecommendationService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: resumeRecommendationServiceName,
	HandlerType: (*ResumeRecommendationServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "RecommendResumes", Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			return unaryHandler[ResumeRecommendationServiceServer, ResumeRecommendationRequest, ResumeRecommendationResponse](srv, ctx, dec, interceptor, resumeRecommendationServiceName, "RecommendResumes", ResumeRecommendationServiceServer.RecommendResumes)
		}},
		{MethodName: "CreateResumeRecommendationTask", Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			return unaryHandler[ResumeRecommendationServiceServer, ResumeRecommendationRequest, ResumeRecommendationTaskResponse](srv, ctx, dec, interceptor, resumeRecommendationServiceName, "CreateResumeRecommendationTask", ResumeRecommendationServiceServer.CreateResumeRecommendationTask)
		}},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "RecommendResumesStream",
			Handler:       resumeRecommendationServiceRecommendResumesStreamHandler,
			ServerStreams: true,
		},
		{
			StreamName:    "WatchResumeRecommendationTask",
			Handler:       resumeRecommendationServiceWatchResumeRecommendationTaskHandler,
			ServerStreams: true,
		},
	},
	Metadata: "recruitment.proto",
}

var LLMGatewayService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: llmGatewayServiceName,
	HandlerType: (*LLMGatewayServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "ListModels", Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			return unaryHandler[LLMGatewayServiceServer, ListLLMModelsRequest, ListLLMModelsResponse](srv, ctx, dec, interceptor, llmGatewayServiceName, "ListModels", LLMGatewayServiceServer.ListModels)
		}},
		{MethodName: "Chat", Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			return unaryHandler[LLMGatewayServiceServer, LLMChatRequest, LLMChatResponse](srv, ctx, dec, interceptor, llmGatewayServiceName, "Chat", LLMGatewayServiceServer.Chat)
		}},
		{MethodName: "Evaluate", Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			return unaryHandler[LLMGatewayServiceServer, LLMEvaluateRequest, LLMEvaluateResponse](srv, ctx, dec, interceptor, llmGatewayServiceName, "Evaluate", LLMGatewayServiceServer.Evaluate)
		}},
	},
	Metadata: "recruitment.proto",
}
