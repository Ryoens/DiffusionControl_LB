// ソケット通信を利用
package main

import (
	"encoding/json"
	"fmt"
	"flag"
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
)

type Server struct {
	IP     string
	Weight int
	Sessions int
	// 追加で各webサーバが持つセッション数を入れるかも
}

type Cluster struct {
	Cluster_LB string `json:"cluster_lb"`
	Web0       string `json:"web0"`
	Web1       string `json:"web1"`
	Web2       string `json:"web2"`
}

type LoadBalancer struct {
	Address   string
	IsHealthy bool
	data      int
	weight    int
	transport int
}

type Response struct {
	TotalQueue int
	CurrentQueue []int
	Data []int
	Weight []int
	Feedback int
	Threshold int
	Kappa float64
}

type Split_data struct {
	Data []int
	Weight []int
}

// グローバル変数の定義
var (
	proxyIPs = []Server{
		{"", 0, 0},
		{"", 0, 0},
		{"", 0, 0},
	}

	clusterLBs []LoadBalancer // 隣接リスト

	randomIndex  Server
	my_clusterLB string
	queue        int // 処理待ちTCPセッション数
	currentIndex int
	wg           sync.WaitGroup
	mutex        sync.RWMutex

	// 評価用パラメータ
	total_queue int
	current_queue []int
	data []int
	weight []int

	final bool
	feedback int
	threshold int
	kappa float64
)

const (
	// 固定値の定義
	tcp_port   string  = ":8001"
	sub_port   string  = ":8002"
	dst_port   string  = ":80"    // webサーバ用
	grpc_dest  string  = ":50051" // gRPCで使用
	sleep_time int     = 1

	logFile = "./log/output.csv"

	healthCheckMsg   = "HEALTH_CHECK"
	controlMsgPrefix = "CONTROL"
)

func init() {
	// 引数の取得
	t := flag.Int("t", 0, "feedback information")
	q := flag.Int("q", 0, "threshold")
	k := flag.Float64("k", 0.0, "diffusion coefficient")

	flag.Parse()
	feedback = *t
    threshold = *q
    kappa = *k

	fmt.Printf("feedback -t : %d\n", feedback)
    fmt.Printf("threshold -q : %d\n", threshold)
    fmt.Printf("kappa -k : %.2f\n", kappa)

	// JSONファイルを開く
	file, err := os.Open("./json/config.json")
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	// JSONファイルの内容を読み込む
	value, err := ioutil.ReadAll(file)
	if err != nil {
		log.Fatalf("Failed to read file: %v", err)
	}

	var clusters map[string]Cluster
	err = json.Unmarshal(value, &clusters)
	if err != nil {
		log.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	// hostname -i を実行
	cmd := exec.Command("hostname", "-i")
	output, err := cmd.Output()
	if err != nil {
		fmt.Printf("Error executing command: %v\n", err)
		return
	}

	// 出力結果をスペースで分割し、配列に代入
	ip_addresses := strings.Fields(string(output))

	// 自身のCluster_LBのIPアドレスを抽出
	clusterLB := ip_addresses[1] // 最初のIPアドレスを取得

	data = make([]int, len(clusterLBs))
	weight = make([]int, len(clusterLBs))

	// 各クラスタのLBのIPアドレスと照合
	for _, cluster := range clusters {
		if clusterLB != cluster.Cluster_LB {
			// 各クラスタLBのIPアドレスをリストに追加
			clusterLBs = append(clusterLBs, LoadBalancer{
				Address:   cluster.Cluster_LB,
				IsHealthy: false,
				data:      0,
				weight:    0,
				transport: 0, 
			})
		} else {
			// proxyIPsにサーバ情報を設定
			proxyIPs[0].IP = cluster.Web0
			proxyIPs[1].IP = cluster.Web1
			proxyIPs[2].IP = cluster.Web2
			my_clusterLB = cluster.Cluster_LB
		}
	}

	// デバック用
	// Cluster2の場合: 10.0.3.10 10.0.3.11 10.0.3.12 114.51.4.4 [114.51.4.2 114.51.4.3]
	fmt.Println(proxyIPs[0].IP, proxyIPs[1].IP, proxyIPs[2].IP, my_clusterLB, clusterLBs)
}

func main(){
	wg.Add(1)
	go Socket_Server()

	// waitgroupを利用し、複数のサーバに接続するたびにプロセスを生成
	for i, address := range clusterLBs {
		wg.Add(1)
		go Socket_Client(address.Address, i)
		fmt.Printf("%d, %v\n", i, address)
	}
	
	wg.Add(1)
	go func() {
		defer wg.Done()
		s := http.Server{
			Addr:    tcp_port,
			Handler: http.HandlerFunc(lbHandler),
		}

		fmt.Printf("HTTP server is listening on %s...\n", tcp_port)
		if err := s.ListenAndServe(); err != nil {
			log.Fatal(err.Error())
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		s := http.Server{
			Addr:    sub_port,
			Handler: http.HandlerFunc(dataReceiver),
		}

		fmt.Printf("HTTP server is listening on %s...\n", sub_port)
		if err := s.ListenAndServe(); err != nil {
			log.Fatal(err.Error())
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		getData()
	}()

	wg.Wait()
}

func getData() {
	for {
		// fmt.Println("loop")

		if final {
			os.Exit(1)
		}
		queue++

		current_queue = append(current_queue, queue)

		for i, server := range clusterLBs {
			data = append(data, server.data)
			weight = append(weight, server.weight)

			fmt.Printf("%d data: %d, weight: %d\n", i, server.data, server.weight)
		}

		time.Sleep(time.Duration(sleep_time) * time.Second)
	}
}

// リクエストをweighted RRで処理
func lbHandler(w http.ResponseWriter, r *http.Request) {
	mutex.Lock()
	total_queue++
	queue++ // 処理待ちセッション数をインクリメント
	mutex.Unlock()

	proxyURL := &url.URL{
		Scheme: "http",
		Host:   "",
	}

	if queue > threshold {
		// Calculate関数で計算した値を該当IPアドレスの重みとして指定
		// randomIndex = WeightedRoundRobin_AdjacentLB()
		// proxyURL.Host = randomIndex.IP + tcp_port
		proxyURL.Host = WeightedRoundRobin_AdjacentLB()
	} else {
		randomIndex = RoundRobin_Backend()
		proxyURL.Host = randomIndex.IP + dst_port
		fmt.Println(proxyURL.Host)
	}

	fmt.Println(queue)

	// デバック用(選択されたIPアドレスの確認)
	fmt.Println("Selected IP:", randomIndex, proxyURL)

	// レスポンスを書き換える
	modifier := func(res *http.Response) error {
		mutex.Lock()
		queue-- // 処理完了後にデクリメント
		mutex.Unlock()
		return nil
	}

	// make reverse proxy
	proxy := httputil.NewSingleHostReverseProxy(proxyURL)
	proxy.ModifyResponse = modifier // ここに入れるとすぐレスポンス返却されてしまう
	proxy.ServeHTTP(w, r)
	// リバースプロキシ後に入れるとカウントがデクリメントされない
}


func dataReceiver(w http.ResponseWriter, r *http.Request) {
	// 負荷テスト終了後に各パラメータのデータを取得
	fmt.Printf("total_request: %d\n", total_queue)
	fmt.Printf("queue_transition: %d\n", current_queue)
	fmt.Printf("total_data: %d\n", data)
	fmt.Printf("total_weight: %d\n", weight)

	// 本来ならクラスタごとのデータを取得したい
	for i := 0; i < len(clusterLBs); i++ {
		fmt.Printf("amount of transport(%s): %d\n", clusterLBs[i].Address, clusterLBs[i].transport)
	}

	clusters := make([]Split_data, len(clusterLBs))

	for i := 0; i < len(data); i++ {
		clusterIndex := i % len(clusterLBs)
		clusters[clusterIndex].Data = append(clusters[clusterIndex].Data, data[i])
		clusters[clusterIndex].Weight = append(clusters[clusterIndex].Weight, weight[i])
	}

	response := Response{
		TotalQueue: total_queue,
		CurrentQueue: current_queue,
		Data: data,
		Weight: weight,
		Feedback: feedback,
		Threshold: threshold,
		Kappa: kappa,
	}

	// --------
	// filename := "../log/output.csv"
	file, err := os.Create(logFile)
	if err != nil {
		fmt.Println("failure creating csv file:", err)
		return
	}
	defer file.Close()

	var csvData strings.Builder
	header := []string{"CurrentQueue"}
	for i := 0; i < len(clusterLBs); i++ {
		header = append(header, fmt.Sprintf("%d_Data", i))
		header = append(header, fmt.Sprintf("%d_Weight", i))
	}
	header = append(header, "TotalQueue")
	header = append(header, "feedback")
	header = append(header, "threshold")
	header = append(header, "kappa")
	// パラメータが増えた場合はcsv出力としてここで追加する
	csvData.WriteString(strings.Join(header, ",") + "\n")

	rowCount := len(response.CurrentQueue)

	for i := 0; i < rowCount; i++ {
		record := []string{fmt.Sprint(response.CurrentQueue[i])}

		for j := 0; j < len(clusterLBs); j++ {
			if i < len(clusters[j].Data) {
				record = append(record, strconv.Itoa(clusters[j].Data[i]))
			} else {
				record = append(record, "0") // データがない場合は0を挿入
			}

			if i < len(clusters[j].Weight) {
				record = append(record, strconv.Itoa(clusters[j].Weight[i]))
			} else {
				record = append(record, "0") // ウェイトがない場合は0を挿入
			}
		}

		// TotalQueueを最初の行にのみ追加
		if i == 0 {
			record = append(record, strconv.Itoa(response.TotalQueue))
			record = append(record, strconv.Itoa(response.Feedback))
			record = append(record, strconv.Itoa(response.Threshold))
			record = append(record, strconv.FormatFloat(response.Kappa, 'f', -1, 64))
		} else {
			record = append(record, "") // それ以外の行には空白を挿入
			record = append(record, "")
			record = append(record, "")
			record = append(record, "")
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
	// os.Exit(1)
} 

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

// クラスタ間の重みづけラウンドロビン(隣接LBへの振り分け)
func WeightedRoundRobin_AdjacentLB() string {
	// 重みは動的に変化した値を取得
	mutex.RLock()
	defer mutex.RUnlock()

	totalWeight := 0
	for _, server := range clusterLBs {
		if !server.IsHealthy {
			server.weight = 0
		}

		totalWeight += server.weight
	}

	// すべての重みが0の場合(どこの隣接LBも空いていないとき)
	if totalWeight == 0 {
		// ポート番号をdst_portに指定 
		tempIndex := RoundRobin_Backend()
		return tempIndex.IP + dst_port
	}

	// 0からtotalWeight-1までの乱数を生成
	rand.Seed(time.Now().UnixNano())
	randomWeight := rand.Intn(totalWeight)

	//fmt.Printf("totalWeight: %d, randomWeight: %d\n", totalWeight, randomWeight)
	// 重みでサーバーを選択
	for i, server := range clusterLBs {
		if randomWeight < server.weight {
			clusterLBs[i].transport++
			// return Server{
			// 	IP:     server.Address,
			// 	Weight: server.weight,
			// }
			return server.Address + tcp_port
		}
		randomWeight -= server.weight
	}

	// ポート番号をdst_portに指定
	tempIndex := RoundRobin_Backend()
	return tempIndex.IP + dst_port
}

// クラスタ内でのラウンドロビン(バックエンドサーバへの振り分け)
func RoundRobin_Backend() Server {
	list := proxyIPs[currentIndex]
	currentIndex = (currentIndex + 1) % len(proxyIPs)

	return list
}

// gRPCサーバ
func Socket_Server() {
	defer wg.Done()

	udpAddr, err := net.ResolveUDPAddr("udp", grpc_dest)
	if err != nil {
		fmt.Println("Error resolving address:", err)
		os.Exit(1)
	}

	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		fmt.Println("Error opening UDP connection:", err)
		os.Exit(1)
	}
	defer conn.Close()

	fmt.Println("Server is listening on", grpc_dest)

	for {
		buffer := make([]byte, 1024)
		n, addr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			fmt.Println("Unable to receive data", err)
			continue
		}
		message := string(buffer[:n])
		fmt.Printf("Received message: %s from %s\n", message, addr)

		// 制御メッセージかどうか判定して応答（payload: queue値を返す）
		if strings.HasPrefix(message, controlMsgPrefix) {
			response := strconv.Itoa(queue)
			_, err = conn.WriteToUDP([]byte(response), addr)
			if err != nil {
				fmt.Println("Error sending response:", err)
			}
			continue
		}

		// その他の通常応答
		response := fmt.Sprintf("Server received: %s", message)
		_, err = conn.WriteToUDP([]byte(response), addr)
		if err != nil {
			fmt.Println("Error sending response:", err)
		}
	}
}

// gRPCクライアント(変更前)
func Socket_Client(address string, i int) {
	defer wg.Done()

	adjacent_lb := address + grpc_dest

	addr, err := net.ResolveUDPAddr("udp", adjacent_lb)
	if err != nil {
		fmt.Println("Failed to resolve address:", err)
		return
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		fmt.Println("Failed to connect to server:", err)
		return
	}
	defer conn.Close()

	ticker := time.NewTicker(time.Duration(feedback) * time.Millisecond)
	defer ticker.Stop()

	buffer := make([]byte, 1024)
	// fmt.Println("buffer")

	for range ticker.C {
		controlMsg := fmt.Sprintf("%s", controlMsgPrefix)
		_, err := conn.Write([]byte(controlMsg))
		if err != nil {
			fmt.Println("Error sending control message:", err)
			return
		}

		n, _, err := conn.ReadFromUDP(buffer)
		if err != nil {
			fmt.Println("Error receiving response:", err)
			return
		}

		responseStr := string(buffer[:n])
		fmt.Printf("Received response: %s\n", responseStr)

		mutex.Lock()
		clusterLBs[i].data, err = strconv.Atoi(responseStr)
		if err != nil {
			fmt.Println("Error converting response to int:", err)
			mutex.Unlock()
			continue
		}

		Calculate(clusterLBs[i].data, i)
		mutex.Unlock()
	}
}

// 隣接LBのフィードバック情報を取得するたびに本関数を呼び出し
// 転送するリクエスト数の計算(重み)
func Calculate(next_queue int, num int) {
	// DC方式で計算
	fmt.Printf("%d, %d calc: ", num, next_queue)

	if queue > next_queue {
		diff := queue - next_queue
		clusterLBs[num].weight = int(math.Round(kappa * float64(diff)))
	} else {
		clusterLBs[num].weight = 0
	}

	fmt.Printf("%d\n", clusterLBs[num].weight)

}