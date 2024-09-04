package main

import (
	"fmt"
	"bufio"
	"log"
	"os"
	"context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	hellopb "grpc_test/pkg/grpc"
)

var (
	scanner *bufio.Scanner
	client	hellopb.GreetingServiceClient
)

func main() {
	fmt.Println("start gRPC Client.")

	// 標準入力から文字列を受け取るスキャナ
	scanner = bufio.NewScanner(os.Stdin)

	// gRPCサーバとのコネクションを確立
	address := "localhost:8081"
	conn, err := grpc.Dial(
		address,

		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		log.Fatal("Connection failed.")
		return
	}
	defer conn.Close()

	// gRPCクライアントを生成
	client = hellopb.NewGreetingServiceClient(conn)

	for {
		fmt.Println("1: send Request")
		fmt.Println("2: exit")
		fmt.Print("please enter >")

		scanner.Scan()
		in := scanner.Text()

		switch in {
		case "1":
			Hello()

		case "2":
			fmt.Println("bye.")
			goto M
		}
	}
M:
}

func Hello() {
	fmt.Println("Please enter your name.")
	scanner.Scan()
	name := scanner.Text()

	req := &hellopb.HelloRequest {
		Name: name, 
	}
	res, err := client.Hello(context.Background(), req)
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(res.GetMessage())
	}
}

