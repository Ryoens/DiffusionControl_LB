// lb_diff.goをベースとして監視機構を追加
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"flag"
	"io"
	"io/ioutil"
	"log"
	"math"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"strconv"
	"sync"
	"time"
	"sort"

	"github.com/redis/go-redis/v9"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "custome_weightedRR/api"
)

type LoadBalancer struct {
	ID int
	Address string
	IsHealthy bool // ヘルスチェック用
	Data int
	Weight int
	Transport int
}

type webServer struct {
	ID int
	IP     string
	Weight int
	Sessions int // 各webサーバのセッション数
}

type ClusterJSON struct {
	AdjacentList map[string]string `json:"adjacentList"`
	InternalList map[string]string `json:"internalList"`
}

type Server struct {
	pb.UnimplementedLoadBalancerServer
}

// 集計データに関して(Response, splitData)
type Response struct {
	TotalQueue []int // 総リクエスト数
	CurrentQueue []int // セッション数
	FirstReceivedQueue []int
	SecondReceivedQueue []int
	CurrentResponse []int // レスポンス数
	CurrentTransport []int // 転送数
	Data []int
	Weight []int
	Transport []int
	Session []int
}

type splitData struct {
	Data []int
	Weight []int
	Transport []int
}

type splitWebServer struct {
	Session []int
}

var (
	clusterLBs []LoadBalancer
	webServers []webServer
	ownWebServers []string
	randomIndex webServer

	wg sync.WaitGroup
	mutex sync.RWMutex

	ctx        = context.Background()
	redisClient *redis.Client
	totalLBs int
	currentIndex int // RR方式におけるインデックス
	ownClusterLB string
	isLeader bool
	flushOnStartup = false
	isTransport bool
	ownNumber string

	firstRecievedIP string
	leaderLB string

	rdb = redis.NewClient(&redis.Options{
		Addr: "10.0.255.2:6379",
		DB:   0,
	})

	transportSet = &http.Transport{
		MaxIdleConns:        1000,
		MaxIdleConnsPerHost: 1000,
		IdleConnTimeout:     90 * time.Second,
	}

	activeSessions = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "active_sessions",
            Help: "現在アクティブなセッション数",
        },
        []string{"cluster", "instance"},
    )
    totalRequests = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "total_requests",
            Help: "合計リクエスト数",
        },
        []string{"cluster", "instance"},
    )

	// 評価用パラメータ
	queue        int // 処理待ちTCPセッション数
	totalQueue int // LBに入ってきた全てのリクエスト
	responseCount    int // レスポンス返却した数をカウント	
	webResponseCount int // 内部のwebサーバにてレスポンス返却した数
	currentTransport int // リバースプロキシで隣接LBに転送した数
	firstReceivedCount int // 隣接LBからダイレクトに受け取ったリクエスト数
	adjacentQueueCount int // マルチホップしたリクエスト

	totalData []int // 
	currentQueue []int
	firstReceivedQueue []int
	secondReceivedQueue []int
	currentResponse []int
	totalTransport []int

	// 隣接LBごとに取得するフィードバック情報
	data []int
	weight []int
	transport []int
	session []int

	final bool
	feedback int
	threshold int
	kappa float64
)	

const (
	tcpPort   string  = ":8001"
	subPort   string  = ":8002"
	dstPort   string  = ":80"    // webサーバ用
	grpcPort  string  = ":50051" // gRPCで使用
	sleepTime time.Duration = 1
	getDataTime time.Duration = 100

	redisHost  = "10.0.255.2:6379"
	redisKey   = "ready:"
	syncChan   = "sync_start"
	logFile = "./log/output.csv"
)

func init(){
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go <positional> -t <value> -q <value> -k <value>")
		os.Exit(1)
	}
	positionalArg := os.Args[1]

	clusterNum, err := strconv.Atoi(positionalArg)
	if err != nil {
		fmt.Println("Invalid positional argument:", positionalArg)
		os.Exit(1)
	}

	flagSet := flag.NewFlagSet("args", flag.ExitOnError)
	
	var t, q int
	var k float64
	ownNumber = positionalArg

	// 引数の取得
	flagSet.IntVar(&t, "t", 0, "feedback information")
	flagSet.IntVar(&q, "q", 0, "threshold")
	flagSet.Float64Var(&k, "k", 0.0, "diffusion coefficient")

	flagSet.Parse(os.Args[2:])
	feedback = t
    threshold = q
    kappa = k

	fmt.Printf("Cluster Number: %d\n", clusterNum)
	fmt.Printf("feedback -t : %d\n", feedback)
    fmt.Printf("threshold -q : %d\n", threshold)
    fmt.Printf("kappa -k : %.2f\n", kappa)

	// open json file 
	file, err := os.Open("./json/adjacentList.json")
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	// read json file
	value, err := ioutil.ReadAll(file)
	if err != nil {
		log.Fatalf("Failed to read file: %v", err)
	}

	clusters := make(map[string]ClusterJSON)
	err = json.Unmarshal(value, &clusters)
	if err != nil {
		log.Fatal(err)
	}
	totalLBs = len(clusters)

	// execute "hostname -i"
	cmd := exec.Command("hostname", "-i")
	output, err := cmd.Output()
	if err != nil {
		panic(err)
	}

	// split output results by spaces and assign to array
	ipAddresses := strings.Fields(string(output))
	// take own Cluster_LB IP address
	if strings.HasPrefix(ipAddresses[0], "172.") {
		ownClusterLB = ipAddresses[0]
	} else {
		ownClusterLB = ipAddresses[1]
	}

	var id, idWeb int
	for _, cluster := range clusters {
		clusterLBIP := cluster.InternalList["cluster_lb"]
		if clusterLBIP == ownClusterLB {
			lastOctet := getLastOctet(clusterLBIP)
			if (lastOctet - clusterNum) == 2 {
				firstRecievedIP = clusterLBIP
				leaderLB = clusterLBIP
			}
			fmt.Println(clusterLBIP, ownClusterLB, firstRecievedIP, leaderLB)
			for _, v := range cluster.AdjacentList {
				// 隣接リストの登録
				clusterLBs = append(clusterLBs, LoadBalancer{
					ID:        id,
					Address:   v,
					IsHealthy: true,
					Data:      0,
					Weight:    0,
					Transport: 0,
				})
				id++
			}
			// 自クラスタの Web サーバ登録
			for k, v := range cluster.InternalList {
				if strings.HasPrefix(k, "web") {
					ownWebServers = append(ownWebServers, v)
				}
			}
		}
	}

	// 自クラスタWebサーバ構造体へ追加
	for _, ip := range ownWebServers {
		webServers = append(webServers, webServer{
			ID:       idWeb,
			IP:       ip,
			Weight:   0,
			Sessions: 0,
		})
		idWeb++
	}

	sort.Slice(clusterLBs, func(i, j int) bool {
		return getLastOctet(clusterLBs[i].Address) < getLastOctet(clusterLBs[j].Address)
	})
	sort.Slice(webServers, func(i, j int) bool {
		return getLastOctet(webServers[i].IP) < getLastOctet(webServers[j].IP)
	})

	for i := range clusterLBs {
		getLastOctet(clusterLBs[i].Address)
		clusterLBs[i].ID = i
	}
	for i := range webServers {
		lastOctet := getLastOctet(webServers[i].IP)
		webServers[i].ID = lastOctet % 10
	}

	fmt.Println(clusterLBs, ownWebServers, webServers)

	// exporterの登録
	prometheus.MustRegister(activeSessions)
	prometheus.MustRegister(totalRequests)
	isLeader = ownClusterLB == leaderLB // リーダーLBだけ true にする
	waitForAllLBsAndSyncStart(ctx, rdb, ownClusterLB, totalLBs, isLeader, "lb_ready:")
}

func main(){
	wg.Add(1)
	go gRPC_Server()

	for i, address := range clusterLBs {
		wg.Add(1)
		go gRPC_Client(address.Address, i) 
	}

	wg.Add(1)
	go func() {
		exporterMux := http.NewServeMux()
		exporterMux.Handle("/federate", promhttp.Handler())
		fmt.Println("Exporter listening on :9090")
		if err := http.ListenAndServe(":9090", exporterMux); err != nil {
			fmt.Printf("Exporter server error: %v\n", err)
		}
	}()

	wg.Add(1)
	go func(){
		defer wg.Done()
		s := http.Server{
			Addr:    tcpPort,
			Handler: http.HandlerFunc(lbHandler),
		}

		fmt.Printf("HTTP server is listening on %s...\n", tcpPort)
		if err := s.ListenAndServe(); err != nil {
			log.Fatal(err.Error())
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		s := http.Server{
			Addr:    subPort,
			Handler: http.HandlerFunc(dataReceiver),
		}

		fmt.Printf("HTTP server is listening on %s...\n", subPort)
		if err := s.ListenAndServe(); err != nil {
			log.Fatal(err.Error())
		}
	}()

	wg.Add(1)
	go func(){
		defer wg.Done()
		for {
			if final {
				os.Exit(1)
			}
	
			totalData = append(totalData, totalQueue)
			currentQueue = append(currentQueue, queue)
			firstReceivedQueue = append(firstReceivedQueue, firstReceivedCount)
			secondReceivedQueue = append(secondReceivedQueue, adjacentQueueCount)
			currentResponse = append(currentResponse, responseCount)
			totalTransport = append(totalTransport, currentTransport)
	
			for _, server := range clusterLBs {
				data = append(data, server.Data)
				weight = append(weight, server.Weight)
				transport = append(transport, server.Transport)
			}
			for _, backend := range webServers {
				session = append(session, backend.Sessions)
			}

			// fmt.Println(totalQueue, queue, responseCount, firstReceivedCount, adjacentQueueCount, currentTransport) // for debug
			time.Sleep(getDataTime * time.Millisecond) // ms
			// time.Sleep(time.Duration(sleep_time) * time.Second) // s
		}
	}()

	wg.Wait()
}

// リクエストをweighted RRで処理
func lbHandler(w http.ResponseWriter, r *http.Request) {
	isTransport = false
	mutex.Lock()
	// activeSessions.Inc()
	activeSessions.WithLabelValues(ownNumber, ownClusterLB).Inc()
	totalRequests.WithLabelValues(ownNumber, ownClusterLB).Inc()
	totalQueue++
	queue++ // 処理待ちセッション数をインクリメント

	originalLB := r.Header.Get("X-Original-LB")
	if originalLB == "" {
		// fmt.Println("source: external user")
	} else if originalLB == firstRecievedIP {
		// fmt.Println("source LB address:", originalLB)
		firstReceivedCount++
	} else {
		// fmt.Println("source adjacentLB address:", originalLB)
		adjacentQueueCount++
	}

	mutex.Unlock()

	proxyURL := &url.URL{
		Scheme: "http",
		Host:   "",
	}

	proxy := httputil.NewSingleHostReverseProxy(proxyURL)
	proxy.Transport = transportSet

	// 閾値が0以上のとき
	if threshold > 0 {
		tempWeight := 0
		for _, info := range clusterLBs {
			tempWeight = queue - info.Data
			if tempWeight > threshold {
				mutex.Lock()
				isTransport = true
				mutex.Unlock()
			}
		}
	} else {
		for _, info := range clusterLBs {
			if info.Weight > 0 {
				mutex.Lock()
				isTransport = true
				mutex.Unlock()
				break
			}
		}
	}

	if isTransport {
		// Calculate関数で計算した値を該当IPアドレスの重みとして指定
		proxyURL.Host = WeightedRoundRobin_AdjacentLB()

		originalDirector := proxy.Director
		proxy.Director = func(req *http.Request) {
			originalDirector(req)
			req.Header.Set("X-Original-LB", ownClusterLB)
		}

		proxy.ModifyResponse = func(res *http.Response) error {
			mutex.Lock()
			// activeSessions.Dec()
			activeSessions.WithLabelValues(ownNumber, ownClusterLB).Dec()
			queue-- // 処理完了後にデクリメント
			currentTransport++
			mutex.Unlock()
			return nil
		}
	} else {
		randomIndex = RoundRobin_Backend()
		proxyURL.Host = randomIndex.IP + dstPort

		// レスポンスを書き換える -> 内部のwebサーバへ送る場合
		proxy.ModifyResponse = func(res *http.Response) error {
			mutex.Lock()
			// activeSessions.Dec()
			activeSessions.WithLabelValues(ownNumber, ownClusterLB).Dec()
			queue-- // 処理完了後にデクリメント
			responseCount++ 
			mutex.Unlock()
			return nil
		}
	}
	proxy.ServeHTTP(w, r)
}

// クラスタ間の重みづけラウンドロビン(隣接LBへの振り分け)
func WeightedRoundRobin_AdjacentLB() string {
	// 重みは動的に変化した値を取得
	mutex.RLock()
	defer mutex.RUnlock()

	totalWeight := 0
	for _, server := range clusterLBs {
		if !server.IsHealthy {
			server.Weight = 0
		}

		totalWeight += server.Weight
	}

	// すべての重みが0の場合(どこの隣接LBも空いていないとき)
	if totalWeight == 0 {
		tempIndex := RoundRobin_Backend()
		return tempIndex.IP + dstPort
	}

	// 0からtotalWeight-1までの乱数を生成
	rand.Seed(time.Now().UnixNano())
	randomWeight := rand.Intn(totalWeight)

	// 重みでサーバーを選択
	for i, server := range clusterLBs {
		if randomWeight < server.Weight {
			clusterLBs[i].Transport++
			return server.Address + tcpPort
		}
		randomWeight -= server.Weight
	}

	// ポート番号をdst_portに指定
	tempIndex := RoundRobin_Backend()
	return tempIndex.IP + dstPort
}

// クラスタ内でのラウンドロビン(バックエンドサーバへの振り分け)
func RoundRobin_Backend() webServer {
	webServers[currentIndex].Sessions++
	list := webServers[currentIndex]
	currentIndex = (currentIndex + 1) % len(webServers)

	return list
}

// gRPCサーバ
func gRPC_Server() {
	defer wg.Done()

	lis, err := net.Listen("tcp", grpcPort)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterLoadBalancerServer(s, &Server{})
	log.Printf("gRPC Server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

// 隣接LBへのヘルスチェック
func (s *Server) GetBackendStatus(ctx context.Context, req *pb.BackendRequest) (*pb.BackendStatus, error) {
	//fmt.Printf("Received health check request for server: %s\n", req.ServerName)
	return &pb.BackendStatus{IsHealthy: true}, nil
}

// 隣接LBへの制御情報の送信
func (s *Server) ControlStream(stream pb.LoadBalancer_ControlStreamServer) error {
	for {
		_, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			log.Printf("Error receiving control message: %v", err)
			return err
		}
		// log.Printf("Received control command: %s, TCP Waiting Sessions: %d", in.Command, queue)

		// 現在の制御情報をクライアントに送信
		if err := stream.Send(&pb.ControlResponse{Status: "ok", Payload: int64(queue)}); err != nil {
			log.Printf("Error sending response: %v", err)
			return err
		}
	}
}

func healthCheck(client pb.LoadBalancerClient, adjacentLB string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(feedback) * time.Millisecond)
	defer cancel()

	req := &pb.BackendRequest{ServerName: "server-1"}
	res, err := client.GetBackendStatus(ctx, req)
	if err != nil || !res.IsHealthy {
		log.Printf("Server %s is not healthy, trying the next one...", adjacentLB)
		return false
	}

	// log.Printf("Server %s is healthy, starting control stream...", adjacent_lb)
	return true
}

// gRPCクライアント(変更前)
func gRPC_Client(address string, i int) {
	defer wg.Done()

	adjacentLB := address + grpcPort

	// サーバとのコネクションを確立
	conn, err := grpc.Dial(adjacentLB, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalf("No connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewLoadBalancerClient(conn)

	if healthCheck(client, adjacentLB) {
		clusterLBs[i].IsHealthy = true
		handleControlStream(client, adjacentLB, i)
	} else {
		clusterLBs[i].IsHealthy = false
		log.Printf("Load Balancer at %s is down", adjacentLB)
		return
	}
	// 複数LBに接続する場合、切り替えに遅延を設定...?
	// time.Sleep(time.Duration(sleep_time) * time.Second)

}

func handleControlStream(client pb.LoadBalancerClient, address string, num int) {
	//defer wg.Done()

	// ここからヘルスチェックでtrueだった場合の処理
	// 双方向ストリーミングの制御情報送受信 (streamを作成)
	stream, err := client.ControlStream(context.Background())
	if err != nil {
		log.Fatalf("Error creating stream: %v", err)
		// log.Printf("Error creating stream: %v", err)
		return
	}

	// 定期的にヘルスチェックと制御情報を送受信
	// ticker := time.NewTicker(time.Duration(sleep_time) * time.Second)
	ticker := time.NewTicker(time.Duration(feedback) * time.Millisecond)
	for range ticker.C {
		// 制御情報の送信
		if err := stream.Send(&pb.ControlMessage{Command: "update_policy", Payload: int64(queue)}); err != nil {
			// log.Printf("Error sending control message: %v", err)
			clusterLBs[num].IsHealthy = false

			if status.Code(err) == codes.Canceled || status.Code(err) == codes.Unavailable {
				log.Printf("Send Connection to %s was lost, reconnecting...", address)
				return
			}
			return
		}

		// 制御情報の応答受信
		in, err := stream.Recv()
		if err != nil {
			log.Printf("Error receiving control response: %v", err)
			clusterLBs[num].IsHealthy = false

			if status.Code(err) == codes.Canceled || status.Code(err) == codes.Unavailable {
				log.Printf("Receive Connection to %s was lost, reconnecting...", address)
				return
			}
			return
		}
		// log.Printf("Received control response: %d", in.Payload)

		mutex.Lock()
		clusterLBs[num].Data = int(in.Payload)

		Calculate(clusterLBs[num].Data, num)
		mutex.Unlock()
	}
}

// 隣接LBのフィードバック情報を取得するたびに本関数を呼び出し
// 転送するリクエスト数の計算(重み)
func Calculate(next_queue int, num int) {
	// DC方式で計算
	if queue > next_queue {
		diff := queue - next_queue
		clusterLBs[num].Weight = int(math.Round(kappa * float64(diff)))
	} else {
		clusterLBs[num].Weight = 0
	}
}

// dataReceiver()
func dataReceiver(w http.ResponseWriter, r *http.Request) {
	// 負荷テスト終了後に各パラメータのデータを取得
	fmt.Printf("total_request: %d\n", totalQueue)
	fmt.Printf("queue_transition: %d\n", currentQueue)
	fmt.Printf("total_data: %d\n", data)
	fmt.Printf("total_weight: %d\n", weight)

	// 本来ならクラスタごとのデータを取得したい
	for i := 0; i < len(clusterLBs); i++ {
		fmt.Printf("amount of transport(%s): %d\n", clusterLBs[i].Address, clusterLBs[i].Transport)
	}

	clusters := make([]splitData, len(clusterLBs))

	for i := 0; i < len(data); i++ {
		clusterIndex := i % len(clusterLBs)
		clusters[clusterIndex].Data = append(clusters[clusterIndex].Data, data[i])
		clusters[clusterIndex].Weight = append(clusters[clusterIndex].Weight, weight[i])
		clusters[clusterIndex].Transport = append(clusters[clusterIndex].Transport, transport[i])
	}

	backends := make([]splitWebServer, len(webServers))
	for i := 0; i < len(session); i++ {
		backendsIndex := i % len(webServers)
		backends[backendsIndex].Session = append(backends[backendsIndex].Session, session[i])
	}

	response := Response {
		TotalQueue: totalData,
		CurrentQueue: currentQueue,
		FirstReceivedQueue: firstReceivedQueue,
		SecondReceivedQueue: secondReceivedQueue,
		CurrentResponse: currentResponse,
		CurrentTransport: totalTransport,
		Data: data,
		Weight: weight,
		Transport: transport,
		Session: session, 
	}

	// --------
	file, err := os.Create(logFile)
	if err != nil {
		fmt.Println("failure creating csv file:", err)
		return
	}
	defer file.Close()

	var csvData strings.Builder
	header := []string{"TotalQueue"}
	header = append(header, "Queue")
	header = append(header, "FirstReceivedQueue")
	header = append(header, "SecondReceivedQueue")
	header = append(header, "CurrentResponse")
	header = append(header, "CurrentTransport")
	for i := 0; i < len(clusterLBs); i++ {
		header = append(header, fmt.Sprintf("%d_Data", clusterLBs[i].ID))
	}
	for i := 0; i < len(clusterLBs); i++ {
		header = append(header, fmt.Sprintf("%d_Weight", clusterLBs[i].ID))
	}
	for i := 0; i < len(clusterLBs); i++ {
		header = append(header, fmt.Sprintf("%d_Transport", clusterLBs[i].ID))
	}
	for i := 0; i < len(webServers); i++ {
		header = append(header, fmt.Sprintf("%d_Session", webServers[i].ID))
	}
	
	// パラメータが増えた場合はcsv出力としてここで追加する
	csvData.WriteString(strings.Join(header, ",") + "\n")

	rowCount := len(response.CurrentQueue)

	for i := 0; i < rowCount; i++ {
		record := []string{fmt.Sprint(response.TotalQueue[i])}
		record = append(record, strconv.Itoa(response.CurrentQueue[i]))
		record = append(record, strconv.Itoa(response.FirstReceivedQueue[i]))
		record = append(record, strconv.Itoa(response.SecondReceivedQueue[i]))
		record = append(record, strconv.Itoa(response.CurrentResponse[i]))
		record = append(record, strconv.Itoa(response.CurrentTransport[i]))
		
		for j := 0; j < len(clusterLBs); j++ {
			if i < len(clusters[j].Data) {
				record = append(record, strconv.Itoa(clusters[j].Data[i]))
			} else {
				record = append(record, "0") // データがない場合は0を挿入
			}
		}
		for j := 0; j < len(clusterLBs); j++ {
			if i < len(clusters[j].Weight) {
				record = append(record, strconv.Itoa(clusters[j].Weight[i]))
			} else {
				record = append(record, "0") // ウェイトがない場合は0を挿入
			}
		}
		for j := 0; j < len(clusterLBs); j++ {
			if i < len(clusters[j].Transport) {
				record = append(record, strconv.Itoa(clusters[j].Transport[i]))
			} else {
				record = append(record, "0") // 値がない場合は0を挿入
			}
		}
		for j := 0; j < len(webServers); j++ {
			if i < len(backends[j].Session) {
				record = append(record, strconv.Itoa(backends[j].Session[i]))
			} else {
				record = append(record, "0") // 値がない場合は0を挿入
			}
		}

		csvData.WriteString(strings.Join(record, ",") + "\n") 
	}

	_, err = file.WriteString(csvData.String())
	if err != nil {
		fmt.Println("Error writing to CSV file:", err)
		return
	}
	// --------
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename="+logFile)

	http.ServeFile(w, r, logFile)
	
	final = true
} 

// joinWeight()
// ウェイトのスライスをカンマ区切りの文字列に変換するヘルパー関数
func joinWeight(weight []int) string {
	var result strings.Builder
	for i, w := range weight {
		if i > 0 {
			result.WriteString(",")
		}
		result.WriteString(strconv.Itoa(w))
	}
	return result.String()
}

func getLastOctet(ip string) int {
    parts := strings.Split(ip, ".")
    if len(parts) != 4 {
        fmt.Println("Invalid IP address format:", ip)
        return -1
    }
    last, err := strconv.Atoi(parts[3])
    if err != nil {
        fmt.Println("Invalid IP address:", ip)
        return -1
    }
    return last
}

func waitForAllLBsAndSyncStart(ctx context.Context, rdb *redis.Client, ownClusterLB string, totalLBs int, isLeader bool, redisKey string) {
	pubsubChannel := "sync_start"

	// 自分の準備完了を通知
	if err := rdb.Set(ctx, redisKey+ownClusterLB, "true", 0).Err(); err != nil {
		log.Fatalf("Redis SET failed: %v", err)
	}
	fmt.Println("LB Ready sent:", redisKey+ownClusterLB)

	// sync_start 購読（リーダーも含めて全LBが購読）
	sub := rdb.Subscribe(ctx, pubsubChannel)
	defer sub.Close()

	// 最初のメッセージ受信を準備
	_, err := sub.Receive(ctx)
	if err != nil {
		log.Fatalf("Failed to subscribe to %s: %v", pubsubChannel, err)
	}
	ch := sub.Channel()

	if isLeader {
		// リーダーの役割：準備が揃ったらsync_startを発信
		fmt.Println("Coordinator waiting for all LB readiness...")
		for {
			keys, err := rdb.Keys(ctx, redisKey+"*").Result()
			if err != nil {
				log.Printf("Redis KEYS error: %v", err)
				time.Sleep(1 * time.Second)
				continue
			}
			if len(keys) == totalLBs {
				fmt.Printf("%d/%d ready. Publishing sync_start...\n", len(keys), totalLBs)
				break
			}
			fmt.Printf("%d/%d ready\n", len(keys), totalLBs)
			time.Sleep(1 * time.Second)
		}

		// sync_start メッセージ送信
		err := rdb.Publish(ctx, pubsubChannel, "start").Err()
		if err != nil {
			log.Fatalf("Failed to publish sync_start: %v", err)
		}
	}

	// 全員 sync_start を待つ（リーダーも含む）
	fmt.Println("Waiting for sync_start signal...")
	for msg := range ch {
		if msg.Payload == "start" {
			fmt.Println("Received sync_start signal. Proceeding to main.")
			break
		}
	}
}