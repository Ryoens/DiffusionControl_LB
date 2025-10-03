package main

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type clusterLB struct {
	Number int
	Weight int
	Count  int
	tempCount int
	Decrement int
}

var (
	number      int
	count       int
	kappa       float64     = 0.5
	sleepTime   time.Duration = 2
	clusterLBs  []clusterLB
	totalWeight int
	wg          sync.WaitGroup
	mu          sync.Mutex
	stopChan    = make(chan os.Signal, 1)
)

func main() {
	fmt.Print("Number of clusters: ")
	fmt.Scan(&number)

	clusterLBs = make([]clusterLB, number-1)
	for i := 0; i < len(clusterLBs); i++ {
		clusterLBs[i] = clusterLB{
			Number: i + 1,
			Weight: 1,
		}
	}

	fmt.Println("初期クラスタ情報:", clusterLBs)

	rand.Seed(time.Now().UnixNano())

	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	// graceful shutdown goroutine
	go func() {
		<-stopChan
		mu.Lock()
		// fmt.Println("最終カウント結果:")
		for _, lb := range clusterLBs {
			fmt.Printf("Cluster %d: Final Count = %d (Weight = %d) Decrement = %d\n", lb.Number, lb.Count, lb.Weight, lb.Decrement)
		}
		mu.Unlock()
		os.Exit(0)
	}()

	wg.Add(3)

	go generateWeight()
	go runRequestDispatcher()
	go decrementCounts()

	wg.Wait()
}

func runRequestDispatcher() {
	defer wg.Done()

	for {
		mu.Lock()
		count = 1000
		localCount := count
		mu.Unlock()

		for c := 0; c < localCount; c++ {
			mu.Lock()
			totalWeight = 0
			for _, lb := range clusterLBs {
				totalWeight += lb.Weight
			}

			if totalWeight == 0 {
				mu.Unlock()
				fmt.Println("totalWeight = 0、sleeping...")
				// time.Sleep(sleepTime * time.Second)
				continue
			}

			randomWeight := rand.Intn(totalWeight)
			for i := range clusterLBs {
				if randomWeight < clusterLBs[i].Weight {
					clusterLBs[i].Count++
					clusterLBs[i].tempCount++
					break
				}
				randomWeight -= clusterLBs[i].Weight
			}
			mu.Unlock()
		}

		mu.Lock()
		for i, lb := range clusterLBs {
			fmt.Printf("Cluster %d (Weight %d) got %d requests, total %d\n", lb.Number, lb.Weight, lb.tempCount, lb.Count)
			clusterLBs[i].tempCount = 0
		}
		mu.Unlock()

		time.Sleep(sleepTime * time.Second)
	}
}

func generateWeight() {
	defer wg.Done()
	for {
		mu.Lock()
		for i := 0; i < len(clusterLBs); i++ {
			if count > clusterLBs[i].Count {
				diff := count - clusterLBs[i].Count
				clusterLBs[i].Weight = int(math.Round(kappa * float64(diff)))
			} else {
				clusterLBs[i].Weight = 0
			}
		}

		// fmt.Println("🔁 Updated Weights:")
		for _, lb := range clusterLBs {
			fmt.Printf("Cluster %d: Weight %d\n", lb.Number, lb.Weight)
		}
		fmt.Println("---")
		mu.Unlock()

		time.Sleep(sleepTime * time.Second)
	}
}

func decrementCounts() {
	defer wg.Done()
	for {
		mu.Lock()
		for i := 0; i < len(clusterLBs); i++ {
			if clusterLBs[i].Count > 1 {
				clusterLBs[i].Count--
				clusterLBs[i].Decrement++
			}
		}
		mu.Unlock()

		fmt.Println("decrement")
		time.Sleep(100 * time.Millisecond)
		// time.Sleep(sleepTime * time.Second) // 任意のデクリメント間隔
	}
}
