// ラウンドロビン方式(静的負荷分散)に基づき隣接LB間で負荷分散
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"flag"
	"io/ioutil"
	"log"
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
)

type LoadBalancer struct {
	ID int
	Address string
	Transport int
}

type webServer struct {
	ID int
	IP string
	Sessions int
}

type Cluster struct {
	Cluster_LB string `json:"Cluster_LB"`
	Webs map[string]string
}

// 集計データに関して(Response, splitData)
type Response struct {
	TotalQueue []int // 総リクエスト数
	CurrentQueue []int // セッション数
	FirstReceivedQueue []int
	SecondReceivedQueue []int
	CurrentResponse []int // レスポンス数
	CurrentTransport []int // 転送数
	Transport []int
	Session []int
}

type splitData struct {
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
	backendIndex int // RR方式におけるインデックス
	adjacentIndex int
	ownClusterLB string
	isLeader bool
	flushOnStartup = false

	rdb = redis.NewClient(&redis.Options{
		Addr: "10.0.255.2:6379",
		DB:   0,
	})

	transportSet = &http.Transport{
		MaxIdleConns:        1000,
		MaxIdleConnsPerHost: 1000,
		IdleConnTimeout:     90 * time.Second,
	}

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
	sleepTime time.Duration = 1
	getDataTime time.Duration = 100

	redisHost  = "10.0.255.2:6379"
	redisKey   = "ready:"
	leaderLB   = "172.18.4.2"
	syncChan   = "sync_start"
	logFile = "./log/output.csv"
)

func init(){
	// 引数の取得
	t := flag.Int("t", 0, "feedback information")
	q := flag.Int("q", 0, "threshold")
	k := flag.Float64("k", 0.0, "diffusion coefficient")

	flag.Parse()
	feedback = *t
    threshold = *q
    kappa = *k

	args := flag.Args()
	if len(args)%2 != 0 {
		fmt.Println("Error: Remaining arguments must be even (half clusters, half web servers).")
		os.Exit(1)
	}

    fmt.Printf("threshold -q : %d\n", threshold)
	fmt.Println("remain: ", args)

	// open json file 
	file, err := os.Open("./json/config.json")
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	// read json file
	value, err := ioutil.ReadAll(file)
	if err != nil {
		log.Fatalf("Failed to read file: %v", err)
	}

	var raw map[string]map[string]string
	if err := json.Unmarshal(value, &raw); err != nil {
		log.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	clusters := make(map[string]Cluster)
	for name, values := range raw {
		cluster := Cluster{
			Webs: make(map[string]string),
		}
		for k, v := range values {
			if k == "cluster_lb" {
				cluster.Cluster_LB = v
			} else if strings.HasPrefix(k, "web") {
				cluster.Webs[k] = v
			}
		}
		clusters[name] = cluster
	}

	totalLBs = len(clusters)

	// execute "hostname -i"
	cmd := exec.Command("hostname", "-i")
	output, err := cmd.Output()
	if err != nil {
		fmt.Printf("Error executing command: %v\n", err)
		return
	}

	// split output results by spaces and assign to array
	ip_addresses := strings.Fields(string(output))
	// take own Cluster_LB IP address
	if strings.HasPrefix(ip_addresses[1], "172.") {
		ownClusterLB = ip_addresses[1]
	} else {
		ownClusterLB = ip_addresses[0]
	}

	fmt.Println(totalLBs, ownClusterLB, ip_addresses)

	var id, idWeb int
	for _, cluster := range clusters {
		if cluster.Cluster_LB == ownClusterLB {
			// 自クラスタの Web サーバを抽出
			for key, ip := range cluster.Webs {
				if strings.HasPrefix(key, "web") {
					ownWebServers = append(ownWebServers, ip)
				}
			}
		} else {
			// 他クラスタの LB を clusterLBs に追加
			clusterLBs = append(clusterLBs, LoadBalancer{
				ID:        id,
				Address:   cluster.Cluster_LB,
				Transport: 0,
			})
			id++
		}
	}

	for _, web := range ownWebServers {
		webServers = append(webServers, webServer{
			ID: idWeb,
			IP: web,
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
		lastOctet := getLastOctet(clusterLBs[i].Address)
		clusterLBs[i].ID = lastOctet - 2
	}
	for i := range webServers {
		lastOctet := getLastOctet(webServers[i].IP)
		webServers[i].ID = lastOctet % 10
	}

	fmt.Println(clusterLBs, ownWebServers, webServers)

	// ここに受け取ったargsからcluster, webで分離してwebサーバ数を減らす
	half := len(args) / 2
	clusterArgs := args[:half]
	webArgs := args[half:]

	fmt.Println(half, clusterArgs, webArgs)

	for i, cls := range clusterArgs {
		fmt.Println(i, cls, webArgs[i])

		// 自身のIPアドレスがwebサーバを減らすべき対象のクラスタと一致する場合
		if (getLastOctet(ownClusterLB) - 2) == i  {
			num, err := strconv.Atoi(webArgs[i])
			if err != nil {
				fmt.Println("Error: invalid webArgs value", webArgs[i])
				continue
			}
			webNum := len(webServers) - num
			fmt.Println("webnum: ", webNum)
			webServers = webServers[:len(webServers)-webNum]
		}
	}

	fmt.Println(webServers)

	isLeader = ownClusterLB == leaderLB // リーダーLBだけ true にする
	waitForAllLBsAndSyncStart(ctx, rdb, ownClusterLB, totalLBs, isLeader, "lb_ready:")
}

func main(){
	wg.Add(1)
	go func() {
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
	go func() {
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
				transport = append(transport, server.Transport)
			}
			for _, backend := range webServers {
				session = append(session, backend.Sessions)
			}
	
			// fmt.Println(totalQueue, queue, responseCount, currentTransport) // for debug
			time.Sleep(getDataTime * time.Millisecond) // ms
			// time.Sleep(time.Duration(sleep_time) * time.Second) // s
		}
	}()

	wg.Wait()
}

// リクエストをweighted RRで処理
func lbHandler(w http.ResponseWriter, r *http.Request) {
	mutex.Lock()
	totalQueue++
	queue++ // 処理待ちセッション数をインクリメント

	originalLB := r.Header.Get("X-Original-LB")
	if originalLB == "" {
		// fmt.Println("source: external user")
	} else if originalLB == "114.51.4.2" {
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

	if queue > threshold {
		// Calculate関数で計算した値を該当IPアドレスの重みとして指定
		randomIndex = RoundRobin_AdjacentLB()
		proxyURL.Host = randomIndex.IP + tcpPort

		originalDirector := proxy.Director
		proxy.Director = func(req *http.Request) {
			originalDirector(req)
			req.Header.Set("X-Original-LB", ownClusterLB)
		}

		proxy.ModifyResponse = func(res *http.Response) error {
			mutex.Lock()
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
			queue-- // 処理完了後にデクリメント
			responseCount++ 
			mutex.Unlock()
			return nil
		}
	}
	proxy.ServeHTTP(w, r)
}

// クラスタ間でのラウンドロビン(隣接LBへの振り分け)
func RoundRobin_AdjacentLB() webServer {
	extenal := clusterLBs[adjacentIndex]
	adjacentIndex = (adjacentIndex + 1) % len(clusterLBs)

	// external: LoadBalancer型 -> webServer型に変換
	return webServer{
		IP:       extenal.Address,
		Sessions: extenal.Transport,
	}
}

// クラスタ内でのラウンドロビン(バックエンドサーバへの振り分け)
func RoundRobin_Backend() webServer {
	webServers[backendIndex].Sessions++
	internal := webServers[backendIndex]
	backendIndex = (backendIndex + 1) % len(webServers)

	return internal
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
		clusters[clusterIndex].Transport = append(clusters[clusterIndex].Transport, transport[i])
	}

	backends := make([]splitWebServer, len(webServers))
	for i := 0; i < len(session); i++ {
		backendsIndex := i % len(webServers)
		backends[backendsIndex].Session = append(backends[backendsIndex].Session, session[i])
	}

	response := Response{
		TotalQueue: totalData,
		CurrentQueue: currentQueue,
		FirstReceivedQueue: firstReceivedQueue,
		SecondReceivedQueue: secondReceivedQueue,
		CurrentResponse: currentResponse,
		CurrentTransport: totalTransport,
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
	// redisKey := "lb_ready:"
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