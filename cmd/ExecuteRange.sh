#!/bin/bash

run_experiment() {
  local threshold="$1"
  local kappa="$2"
  local safe_kappa=$(echo "$kappa" | sed 's/\./_/g')
  local time=60
  local attempt_count=1

  local dirname="${compiled_file}_t${threshold}_k${safe_kappa}_vus${vus}"
  local data_dir="../../data/implement/${dirname}"
  mkdir -p "$data_dir"

  echo "Running experiment with threshold=$threshold, kappa=$kappa"

  while [ $attempt_count -le $attempt ]; do
    echo "Attempt $attempt_count"
    count=0

    # LB 実行
    for count in $(seq 0 "$KEY"); do
      docker exec -d Cluster${count}_LB compiled/$compiled_file $cluster -t $feedback -q $threshold -k $kappa
      docker exec Cluster${count}_LB ps aux
    done

    # Redisキーの待機
    while true; do
      key_list=$(docker exec -i redis-server redis-cli --raw keys 'lb_ready:*')
      key_count=$(echo "$key_list" | tr ' ' '\n' | grep -c '^lb_ready:')
      echo "waiting... $key_count/$((KEY + 1))"
      if [ "$key_count" -eq $((KEY + 1)) ]; then
        break
      fi
      sleep 1
    done

    # JMeter 実行
    ./../tools/jmeter_multi.sh $url $time $vus $KEY
    wait
    echo "All tests completed."

    # データ取得
    for num in $(seq 2 $((KEY + 2))); do 
      i=$((num - 2))
      timestamp=$(date +"%Y%m%d_%H%M%S")
      curl -X GET "172.18.4.${num}:8002" -o "${data_dir}/Cluster${i}_${attempt_count}_${timestamp}.csv"
    done

    # JMeterログ移動
    timestamp=$(date +"%Y%m%d_%H%M%S")
    rm -f ../log/output.csv temp_test.jmx
    mv ../log/jmeter.log "${data_dir}/jmeter_${attempt_count}_${timestamp}.log"
    mv ../log/result_60s.jtl "${data_dir}/jmeter_result${time}s_${attempt_count}_${timestamp}.jtl"

    # Redis初期化
    docker exec -it redis-server redis-cli flushall
    attempt_count=$((attempt_count + 1))
    sleep 5
  done

  # 結果整形
  python3 ../tools/to_average.py $data_dir $KEY
  python3 ../tools/to_median.py $data_dir $KEY

  # パラメータ記録
  timestamp=$(date +"%Y%m%d_%H%M%S")
  {
    echo "Experiment in these parameters is finished"
    echo "feedback: $feedback [ms]"
    echo "threshold: $threshold"
    echo "kappa: $kappa"
    echo "virtual users: $vus [users]"
    echo "network model: $nw_model"
  } > "${data_dir}/parameters_${timestamp}.txt"
}

# ----------------------

count=0
attempt_count=1
time=60
source ~/.bashrc

# 各パラメータの入力
read -p "feedback [ms]: " feedback 
read -p "threshold [int: start, end, step]: " threshold_input
read -p "kappa [float: start, end, step]: " kappa_input
read -p "concurrent (vus): " vus
read -p "number of attempts [5 times]: " attempt
read -p "NW model [f: fullmesh, r: random, ba: balabasi and albert]: " nw_model
read -p "Number of Clusters to reduce Web Servers: " num_cluster
echo "-------- parameter OK --------"

# webサーバ数の指定
if [[ $num_cluster -eq 0 ]]; then
  echo "default start"
elif [[ $num_cluster -eq 1 ]]; then
  read -p "Set number [Cluster] [WebServers]: " cls web
else
  cls=()
  web=()
  for num_cls in $(seq 0 $((num_cluster - 1)) )
  do
    read -p "Set number [Cluster] [WebServers]: " temp_cls temp_web
    cls+=($temp_cls)
    web+=($temp_web)
  done
fi
echo ${cls[@]} ${web[@]}

# フラッシュクラウド対象サーバの指定
container=$(docker ps --filter "name=_LB" --format "{{.Names}}" | head -n 1)
echo $container
KEY=$(echo "$container" | sed -E 's/^Cluster([0-9]+)_LB$/\1/')
echo "number of clusters: $KEY"

read -p "Target Cluster for Flash Crowds [0:$KEY]: " cluster
if ! [[ "$cluster" =~ ^[0-9]+$ ]] || [ "$cluster" -ge "$KEY" ]; then
  echo "Invalid input. Please enter a number between 0 and $((KEY - 1))."
  exit 1
fi

ip_last=$((2 + cluster))
url="http://172.18.4.${ip_last}:8001"
echo "Target URL: $url"

# 配列展開
threshold_parts=($threshold_input)

if [[ ${#threshold_parts[@]} -eq 1 ]]; then
  threshold_values=("${threshold_parts[0]}")
else
  threshold_values=($(seq "${threshold_parts[0]}" "${threshold_parts[2]}" "${threshold_parts[1]}"))
fi

kappa_parts=($kappa_input)

if [[ ${#kappa_parts[@]} -eq 1 ]]; then
  kappa_values=("${kappa_parts[0]}")
else
  kappa_values=($(awk -v start="${kappa_parts[0]}" -v end="${kappa_parts[1]}" -v step="${kappa_parts[2]}" 'BEGIN {
    for (i = start; i <= end + 0.0001; i += step) printf "%.3f\n", i
  }'))
fi

echo $threshold_values $kappa_values
# exit 1

# 対象ファイルの指定
flag=0
if [[ ${#kappa_values[@]} -gt 1 ]]; then
  apply_file="lb_diff.go"
  echo "Selected lb_diff.go"
  flag=1
elif [[ ${#threshold_values[@]} -gt 1 ]]; then
  apply_file="lb_rr.go"
  echo "Selected lb_rr.go"
  flag=2
else
  read -p "file to apply [t: DC(threshold), d: DC(diff), r: RR, l: LC]: " file
  case "$file" in
    t) apply_file="lb_thre.go" ;;
    d) apply_file="lb_diff.go" ;;
    r) apply_file="lb_rr.go" ;;
    l) apply_file="lb_lc.go" ;;
    *) echo "Invalid input."; exit 1 ;;
  esac
fi

compiled_file="${apply_file%.go}"
echo $apply_file $compiled_file

# 隣接リストの作成
python3 ../tools/adjacentListController.py $nw_model ${cls[@]} ${web[@]}
echo "Created adjacentList per cluster"

count=0

# nameserverの設定, build
for count in $(seq 0 "$KEY");
do 
    docker exec Cluster${count}_LB sh -c 'echo "nameserver 8.8.8.8" > /etc/resolv.conf'
    docker exec Cluster${count}_LB cat /etc/resolv.conf
    docker exec Cluster${count}_LB sh -c "go build -o compiled/$compiled_file lb/$apply_file"
done

echo "-------- Build OK --------"

for count in $(seq 0 "$KEY");
do
    docker exec Cluster${count}_LB ls -l compiled/$compiled_file
    docker exec Cluster${count}_LB ps aux
done

sleep 5

if [[ $flag -eq 1 ]]; then
    for kappa in ${kappa_values[@]}; do
        echo "=== kappa=$kappa ==="
        run_experiment ${threshold_values[0]} $kappa
    done
elif [[ $flag -eq 2 ]]; then
    for threshold in ${threshold_values[@]}; do
        echo "=== threshold=$threshold ==="
        run_experiment $threshold ${kappa_values[0]}
    done
else
    threshold=${threshold_values[0]}
    kappa=${kappa_values[0]}
    echo "=== threshold=$threshold, kappa=$kappa ==="
    run_experiment $threshold $kappa
    echo "end"
fi
