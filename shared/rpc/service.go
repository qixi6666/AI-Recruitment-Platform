package rpc

import (
	"context"

	"google.golang.org/grpc"
)

type LogicServiceClient interface {
	Register(ctx context.Context, in *RegisterRequest, opts ...grpc.CallOption) (*RegisterResponse, error)
	Login(ctx context.Context, in *LoginRequest, opts ...grpc.CallOption) (*LoginResponse, error)
	ListJobs(ctx context.Context, in *ListJobsRequest, opts ...grpc.CallOption) (*ListJobsResponse, error)
	CreateJob(ctx context.Context, in *CreateJobRequest, opts ...grpc.CallOption) (*JobResponse, error)
	UpdateJob(ctx context.Context, in *UpdateJobRequest, opts ...grpc.CallOption) (*JobResponse, error)
	OfflineJob(ctx context.Context, in *OfflineJobRequest, opts ...grpc.CallOption) (*JobResponse, error)
	GetProfile(ctx context.Context, in *GetProfileRequest, opts ...grpc.CallOption) (*ProfileResponse, error)
	UpsertProfile(ctx context.Context, in *UpsertProfileRequest, opts ...grpc.CallOption) (*ProfileResponse, error)
	SaveResume(ctx context.Context, in *SaveResumeRequest, opts ...grpc.CallOption) (*ResumeResponse, error)
	GetResume(ctx context.Context, in *GetResumeRequest, opts ...grpc.CallOption) (*ResumeResponse, error)
	ApplyJob(ctx context.Context, in *ApplyJobRequest, opts ...grpc.CallOption) (*ApplyJobResponse, error)
	ListApplications(ctx context.Context, in *ListApplicationsRequest, opts ...grpc.CallOption) (*ListApplicationsResponse, error)
	AIChat(ctx context.Context, in *AIChatRequest, opts ...grpc.CallOption) (*AIChatResponse, error)
	AIChatStream(ctx context.Context, in *AIChatRequest, opts ...grpc.CallOption) (LogicService_AIChatStreamClient, error)
	ListChatHistory(ctx context.Context, in *ListChatHistoryRequest, opts ...grpc.CallOption) (*ListChatHistoryResponse, error)
}

type logicServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewLogicServiceClient(cc grpc.ClientConnInterface) LogicServiceClient {
	return &logicServiceClient{cc: cc}
}

func (c *logicServiceClient) Register(ctx context.Context, in *RegisterRequest, opts ...grpc.CallOption) (*RegisterResponse, error) {
	out := new(RegisterResponse)
	err := c.cc.Invoke(ctx, "/recruitment.LogicService/Register", in, out, opts...)
	return out, err
}

func (c *logicServiceClient) Login(ctx context.Context, in *LoginRequest, opts ...grpc.CallOption) (*LoginResponse, error) {
	out := new(LoginResponse)
	err := c.cc.Invoke(ctx, "/recruitment.LogicService/Login", in, out, opts...)
	return out, err
}

func (c *logicServiceClient) ListJobs(ctx context.Context, in *ListJobsRequest, opts ...grpc.CallOption) (*ListJobsResponse, error) {
	out := new(ListJobsResponse)
	err := c.cc.Invoke(ctx, "/recruitment.LogicService/ListJobs", in, out, opts...)
	return out, err
}

func (c *logicServiceClient) CreateJob(ctx context.Context, in *CreateJobRequest, opts ...grpc.CallOption) (*JobResponse, error) {
	out := new(JobResponse)
	err := c.cc.Invoke(ctx, "/recruitment.LogicService/CreateJob", in, out, opts...)
	return out, err
}

func (c *logicServiceClient) UpdateJob(ctx context.Context, in *UpdateJobRequest, opts ...grpc.CallOption) (*JobResponse, error) {
	out := new(JobResponse)
	err := c.cc.Invoke(ctx, "/recruitment.LogicService/UpdateJob", in, out, opts...)
	return out, err
}

func (c *logicServiceClient) OfflineJob(ctx context.Context, in *OfflineJobRequest, opts ...grpc.CallOption) (*JobResponse, error) {
	out := new(JobResponse)
	err := c.cc.Invoke(ctx, "/recruitment.LogicService/OfflineJob", in, out, opts...)
	return out, err
}

func (c *logicServiceClient) GetProfile(ctx context.Context, in *GetProfileRequest, opts ...grpc.CallOption) (*ProfileResponse, error) {
	out := new(ProfileResponse)
	err := c.cc.Invoke(ctx, "/recruitment.LogicService/GetProfile", in, out, opts...)
	return out, err
}

func (c *logicServiceClient) UpsertProfile(ctx context.Context, in *UpsertProfileRequest, opts ...grpc.CallOption) (*ProfileResponse, error) {
	out := new(ProfileResponse)
	err := c.cc.Invoke(ctx, "/recruitment.LogicService/UpsertProfile", in, out, opts...)
	return out, err
}

func (c *logicServiceClient) SaveResume(ctx context.Context, in *SaveResumeRequest, opts ...grpc.CallOption) (*ResumeResponse, error) {
	out := new(ResumeResponse)
	err := c.cc.Invoke(ctx, "/recruitment.LogicService/SaveResume", in, out, opts...)
	return out, err
}

func (c *logicServiceClient) GetResume(ctx context.Context, in *GetResumeRequest, opts ...grpc.CallOption) (*ResumeResponse, error) {
	out := new(ResumeResponse)
	err := c.cc.Invoke(ctx, "/recruitment.LogicService/GetResume", in, out, opts...)
	return out, err
}

func (c *logicServiceClient) ApplyJob(ctx context.Context, in *ApplyJobRequest, opts ...grpc.CallOption) (*ApplyJobResponse, error) {
	out := new(ApplyJobResponse)
	err := c.cc.Invoke(ctx, "/recruitment.LogicService/ApplyJob", in, out, opts...)
	return out, err
}

func (c *logicServiceClient) ListApplications(ctx context.Context, in *ListApplicationsRequest, opts ...grpc.CallOption) (*ListApplicationsResponse, error) {
	out := new(ListApplicationsResponse)
	err := c.cc.Invoke(ctx, "/recruitment.LogicService/ListApplications", in, out, opts...)
	return out, err
}

func (c *logicServiceClient) AIChat(ctx context.Context, in *AIChatRequest, opts ...grpc.CallOption) (*AIChatResponse, error) {
	out := new(AIChatResponse)
	err := c.cc.Invoke(ctx, "/recruitment.LogicService/AIChat", in, out, opts...)
	return out, err
}

func (c *logicServiceClient) AIChatStream(ctx context.Context, in *AIChatRequest, opts ...grpc.CallOption) (LogicService_AIChatStreamClient, error) {
	stream, err := c.cc.NewStream(ctx, &LogicService_ServiceDesc.Streams[0], "/recruitment.LogicService/AIChatStream", opts...)
	if err != nil {
		return nil, err
	}
	x := &logicServiceAIChatStreamClient{ClientStream: stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type LogicService_AIChatStreamClient interface {
	Recv() (*AIChatStreamChunk, error)
	grpc.ClientStream
}

type logicServiceAIChatStreamClient struct {
	grpc.ClientStream
}

func (x *logicServiceAIChatStreamClient) Recv() (*AIChatStreamChunk, error) {
	m := new(AIChatStreamChunk)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *logicServiceClient) ListChatHistory(ctx context.Context, in *ListChatHistoryRequest, opts ...grpc.CallOption) (*ListChatHistoryResponse, error) {
	out := new(ListChatHistoryResponse)
	err := c.cc.Invoke(ctx, "/recruitment.LogicService/ListChatHistory", in, out, opts...)
	return out, err
}

type LogicServiceServer interface {
	Register(context.Context, *RegisterRequest) (*RegisterResponse, error)
	Login(context.Context, *LoginRequest) (*LoginResponse, error)
	ListJobs(context.Context, *ListJobsRequest) (*ListJobsResponse, error)
	CreateJob(context.Context, *CreateJobRequest) (*JobResponse, error)
	UpdateJob(context.Context, *UpdateJobRequest) (*JobResponse, error)
	OfflineJob(context.Context, *OfflineJobRequest) (*JobResponse, error)
	GetProfile(context.Context, *GetProfileRequest) (*ProfileResponse, error)
	UpsertProfile(context.Context, *UpsertProfileRequest) (*ProfileResponse, error)
	SaveResume(context.Context, *SaveResumeRequest) (*ResumeResponse, error)
	GetResume(context.Context, *GetResumeRequest) (*ResumeResponse, error)
	ApplyJob(context.Context, *ApplyJobRequest) (*ApplyJobResponse, error)
	ListApplications(context.Context, *ListApplicationsRequest) (*ListApplicationsResponse, error)
	AIChat(context.Context, *AIChatRequest) (*AIChatResponse, error)
	AIChatStream(*AIChatRequest, LogicService_AIChatStreamServer) error
	ListChatHistory(context.Context, *ListChatHistoryRequest) (*ListChatHistoryResponse, error)
}

func RegisterLogicServiceServer(s grpc.ServiceRegistrar, srv LogicServiceServer) {
	s.RegisterService(&LogicService_ServiceDesc, srv)
}

func unaryHandler[Req any, Resp any](srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor, method string, fn func(LogicServiceServer, context.Context, *Req) (*Resp, error)) (any, error) {
	in := new(Req)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return fn(srv.(LogicServiceServer), ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/recruitment.LogicService/" + method}
	handler := func(ctx context.Context, req any) (any, error) {
		return fn(srv.(LogicServiceServer), ctx, req.(*Req))
	}
	return interceptor(ctx, in, info, handler)
}

func logicServiceAIChatStreamHandler(srv any, stream grpc.ServerStream) error {
	m := new(AIChatRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(LogicServiceServer).AIChatStream(m, &logicServiceAIChatStreamServer{ServerStream: stream})
}

type LogicService_AIChatStreamServer interface {
	Send(*AIChatStreamChunk) error
	grpc.ServerStream
}

type logicServiceAIChatStreamServer struct {
	grpc.ServerStream
}

func (x *logicServiceAIChatStreamServer) Send(m *AIChatStreamChunk) error {
	return x.ServerStream.SendMsg(m)
}

var LogicService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "recruitment.LogicService",
	HandlerType: (*LogicServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "Register", Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			return unaryHandler[RegisterRequest, RegisterResponse](srv, ctx, dec, interceptor, "Register", LogicServiceServer.Register)
		}},
		{MethodName: "Login", Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			return unaryHandler[LoginRequest, LoginResponse](srv, ctx, dec, interceptor, "Login", LogicServiceServer.Login)
		}},
		{MethodName: "ListJobs", Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			return unaryHandler[ListJobsRequest, ListJobsResponse](srv, ctx, dec, interceptor, "ListJobs", LogicServiceServer.ListJobs)
		}},
		{MethodName: "CreateJob", Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			return unaryHandler[CreateJobRequest, JobResponse](srv, ctx, dec, interceptor, "CreateJob", LogicServiceServer.CreateJob)
		}},
		{MethodName: "UpdateJob", Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			return unaryHandler[UpdateJobRequest, JobResponse](srv, ctx, dec, interceptor, "UpdateJob", LogicServiceServer.UpdateJob)
		}},
		{MethodName: "OfflineJob", Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			return unaryHandler[OfflineJobRequest, JobResponse](srv, ctx, dec, interceptor, "OfflineJob", LogicServiceServer.OfflineJob)
		}},
		{MethodName: "GetProfile", Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			return unaryHandler[GetProfileRequest, ProfileResponse](srv, ctx, dec, interceptor, "GetProfile", LogicServiceServer.GetProfile)
		}},
		{MethodName: "UpsertProfile", Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			return unaryHandler[UpsertProfileRequest, ProfileResponse](srv, ctx, dec, interceptor, "UpsertProfile", LogicServiceServer.UpsertProfile)
		}},
		{MethodName: "SaveResume", Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			return unaryHandler[SaveResumeRequest, ResumeResponse](srv, ctx, dec, interceptor, "SaveResume", LogicServiceServer.SaveResume)
		}},
		{MethodName: "GetResume", Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			return unaryHandler[GetResumeRequest, ResumeResponse](srv, ctx, dec, interceptor, "GetResume", LogicServiceServer.GetResume)
		}},
		{MethodName: "ApplyJob", Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			return unaryHandler[ApplyJobRequest, ApplyJobResponse](srv, ctx, dec, interceptor, "ApplyJob", LogicServiceServer.ApplyJob)
		}},
		{MethodName: "ListApplications", Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			return unaryHandler[ListApplicationsRequest, ListApplicationsResponse](srv, ctx, dec, interceptor, "ListApplications", LogicServiceServer.ListApplications)
		}},
		{MethodName: "AIChat", Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			return unaryHandler[AIChatRequest, AIChatResponse](srv, ctx, dec, interceptor, "AIChat", LogicServiceServer.AIChat)
		}},
		{MethodName: "ListChatHistory", Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			return unaryHandler[ListChatHistoryRequest, ListChatHistoryResponse](srv, ctx, dec, interceptor, "ListChatHistory", LogicServiceServer.ListChatHistory)
		}},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "AIChatStream",
			Handler:       logicServiceAIChatStreamHandler,
			ServerStreams: true,
		},
	},
	Metadata: "recruitment.proto",
}
