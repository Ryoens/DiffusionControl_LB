#!/bin/bash
count=0

# 各パラメータの入力
read -p "feedback: " feedback
read -p "threshold: " threshold
read -p "kappa: " kappa

echo $feedback $threshold $kappa

echo "-------- parameter OK --------"

# クラスタ数をコンテナ数から取得
container=$(docker ps --filter "name=_LB" --format "{{.Names}}" | head -n 1)
KEY=${container:7:1}

echo "number of clusters: " $KEY

# go run mirror.go -t $feedback -q $threshold -k $kappa
# プログラムの実行
while [ $count -le $KEY ]
do
    docker exec -d Cluster${count}_LB go run mirror.go -t $feedback -q $threshold -k $kappa &
    docker exec Cluster${count}_LB ps aux
    count=`expr $count + 1`
done

# 実験データの取得
read -p "仮想ユーザ数: " vus
echo $vus

k6 run ../test.js --vus $vus

for num in $(seq 2 $((count + 1)))
do 
    i=$((num - 2))
    echo Cluster$i 
    curl -X GET 114.51.4.$num:8002; 
done
