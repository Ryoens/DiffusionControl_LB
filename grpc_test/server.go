package main

import (
	"fmt"
	"os"
	"log"
	"net"
	"context"
	"os/signal"
	"google.golang.org/grpc"
	hellopb "grpc_test/pkg/grpc"
)

type myServer struct {
	hellopb.UnimplementedGreetingServiceServer
}

func (s *myServer) Hello(ctx context.Context, req *hellopb.HelloRequest) (*hellopb.HelloResponse, error) {
	// リクエストからnameフィールドを取り出して
	// "Hello, [名前]!"というレスポンスを返す
	return &hellopb.HelloResponse{
		Message: fmt.Sprintf("Hello, %s!", req.GetName()),
	}, nil
}

func NewMyServer() *myServer{
	return &myServer{}
}

func main() {
	// portのlistenerを作成
	port := 8081
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		panic(err)
	}

	// gRPCサーバを作成
	s := grpc.NewServer()

	// gRPCサーバにGreetingServiceを登録
	hellopb.RegisterGreetingServiceServer(s, NewMyServer())

	// gRPCサーバをポートと紐付け
	go func() {
		log.Printf("start gRPC server port: %v", port)
		s.Serve(listener)
	}()

	// ctrl + c でshutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit
	log.Println("stopping gRPC server")
	s.GracefulStop()
}