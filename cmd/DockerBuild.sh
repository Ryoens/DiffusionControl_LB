#!/bin/bash

function json_output () {
	container_name="Cluster$((KEY))_LB"

  	IP_Addresses=$(docker exec $container_name hostname -i)
  	echo $IP_Addresses

  	IP_Array=($IP_Addresses)

  	cat <<EOF >> $JSON_FILE
  	"cluster$KEY": {
    	"cluster_lb": "${IP_Array[1]}",
    	"web0": "10.0.$((KEY+1)).10",
    	"web1": "10.0.$((KEY+1)).11",
    	"web2": "10.0.$((KEY+1)).12"
  	}$(if [ $KEY -lt $QTY_CLUSTER ]; then echo ","; fi)
EOF

  	echo "IP addresses saved to cluster_ips.json"
}

# ----------------------

KEY=0
JSON_FILE="../json/config.json"
QTY_CLUSTER=$(($1))

image_exists=$(docker image ls | grep lb | wc -l)
echo $image_exists

if [ "$image_exists" -eq 0 ]; then
	echo "make image lb"
	docker image build ./ -t lb:latest
else 
	echo "image lb exists"
fi

docker network rm gushing-ecstasy
docker network create gushing-ecstasy --driver=bridge --subnet=114.51.4.0/24

protoc --version
echo $QTY_CLUSTER
pwd

echo "{" > $JSON_FILE

while [ $KEY -le $QTY_CLUSTER ]
do
	sudo mkdir -p ./cluster$KEY
	sudo chmod 777 ./cluster$KEY
	sudo sed -e "s/@num@/$((KEY))/g" -e "s/@num+1@/$((KEY+1))/g" ./docker-compose.yml > ./cluster$KEY/docker-compose.yml

	cd ./cluster$KEY
	docker compose up -d
	cd ../
	rm -rf ./cluster$KEY

	# クラスタごとのIPアドレスをJSONで保持
	json_output

	KEY=`expr $KEY + 1`
done

echo "}" >> $JSON_FILE

# コンテナごとにCPUコアを割り当てる
total_cores=$(nproc --all)
core_index=0

nginx_containers=()
lb_containers=()

for container in $(docker ps --format "{{.Names}}"); do
  # Nginx コンテナを探す (コンテナ名に "web" を含むもの)
  if [[ $container == *web* ]]; then
    nginx_containers+=("$container")
  # LB コンテナを探す (コンテナ名に "LB" を含むもの)
  elif [[ $container == *LB* ]]; then
    lb_containers+=("$container")
  fi
done

# コンテナとそのコア割り当てを表示
echo "コンテナとそのコア割り当て:"

# Nginxコンテナにコアを割り当て
for ((i=0; i<${#nginx_containers[@]}; i++)); do
  # echo "${nginx_containers[i]}: $core_index"
  docker update --cpuset-cpus="$core_index" "${nginx_containers[i]}"
  sleep 1
  
  # 3つごとに次のコアに進める
  if (( (i + 1) % 3 == 0 )); then
    core_index=$((core_index + 1))
  fi
done

# LBコンテナにコアを割り当て
for lb in "${lb_containers[@]}"; do
  # echo "$lb: $core_index"  # Nginxの次のコアを使用
  docker container update --cpuset-cpus="$core_index" "$lb"
  core_index=$((core_index + 1))  # 次のコアに進める
  sleep 1
done

