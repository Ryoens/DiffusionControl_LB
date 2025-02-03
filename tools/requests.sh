#!/bin/bash

# 複数のLBに対して負荷をかけるときに使うかも？
# URLのリスト
urls=(
    "http://114.51.4.2:8001/"
    "http://114.51.4.3:8001/"
    "http://114.51.4.4:8001/"
    "http://114.51.4.5:8001/"
    "http://114.51.4.6:8001/"
)

# 各URLに対して負荷テストを実施
for url in "${urls[@]}"; do
    echo "Testing $url"
    ab -n 100000 -c 10 "$url" &
done

# 全てのプロセスが終了するのを待つ
wait

echo "All tests completed."

KEY=4; 
feedback="100"; 
kappa="0.07"; 
count=0; 
while [ $count -le $KEY ]; 
do 
    docker exec -d Cluster${count}_LB go run main.go -t $feedback -k $kappa /bin/bash; 
    while ! docker exec Cluster${count}_LB ps aux | grep -q "[g]o run main.go"; 
    do 
        echo "Retrying Cluster${count}_LB..."; 
        docker exec -d Cluster${count}_LB go run main.go -t $feedback -k $kappa /bin/bash; 
        sleep 1; 
    done; 
    count=$((count + 1)); 
done