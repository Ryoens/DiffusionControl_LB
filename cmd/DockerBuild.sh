#!/bin/bash

function json_output () {
  container_name="Cluster$((KEY))_LB"
  IP_Addresses=$(docker exec $container_name hostname -i)
  IP_Array=($IP_Addresses)

  if [[ ${IP_Array[1]} == 172.* ]]; then
        cluster_ip=${IP_Array[1]}
    else
        cluster_ip=${IP_Array[0]}  
    fi
	echo $cluster_ip

  echo "  \"cluster$KEY\": {" >> $JSON_FILE
  echo "    \"cluster_lb\": \"$cluster_ip\"," >> $JSON_FILE

  for j in $(seq 0 $((WEB_COUNT - 1)))
  do
    comma=","
    if [ "$j" -eq $((WEB_COUNT - 1)) ]; then
      comma=""
    fi
    echo "    \"web$j\": \"10.0.$((KEY+1)).$((10 + j))\"$comma" >> $JSON_FILE
  done

  if [ "$KEY" -lt $QTY_CLUSTER ]; then
    echo "  }," >> $JSON_FILE
  else
    echo "  }" >> $JSON_FILE
  fi

  echo "IP addresses saved to $JSON_FILE"
}

# ----------------------

KEY=0
JSON_FILE="../json/config.json"
QTY_CLUSTER=$(($1))

read -p "Default Web Servers: " WEB_COUNT 

image_exists=$(docker image ls | grep lb | wc -l)
echo $image_exists

cd ..
path=$(pwd)
cd cmd

if [ "$image_exists" -eq 0 ]; then
	echo "make image lb"
	docker image build ./ -t lb:latest
else 
	echo "image lb exists"
fi

docker network create overlay-net --driver=bridge --subnet=172.18.4.0/24
docker network create deployment --driver=bridge --subnet=10.0.255.0/24
# redis
docker run -d --name redis-server -p 6379:6379 redis:latest
docker network connect deployment redis-server
# prometheus
docker run -d --name prometheus-federate -p 9090:9090 -v $path/prometheus/federation/prometheus.yml:/etc/prometheus/prometheus.yml prom/prometheus
docker network connect deployment prometheus-federate
# grafana
docker run -d --name=grafana -p 3000:3000 grafana/grafana
docker network connect deployment grafana

protoc --version
echo $QTY_CLUSTER
pwd

echo "{" > $JSON_FILE

while [ $KEY -le $QTY_CLUSTER ]
do
    echo "クラスタ$KEY のdocker-compose.ymlを生成中..."

    mkdir -p ./cluster$KEY
    chmod 777 ./cluster$KEY

    cat > ./cluster$KEY/compose.yml <<EOF
version: '3.9'
services:
  LB:
    image: lb
    container_name: Cluster${KEY}_LB
    ports:
      - :8001
    volumes:
      - $path/:/home/ubuntu
    networks:
      overlay-net:
      cluster${KEY}-net:
        ipv4_address: 10.0.$((KEY+1)).2
    tty: true

EOF

    for j in $(seq 0 $((WEB_COUNT - 1)))
    do
        cat >> ./cluster$KEY/compose.yml <<EOW

  web${j}:
    image: nginx
    container_name: Cluster${KEY}_web${j}
    environment:
      - TZ=Asia/Tokyo
      - BUNDLE_APP_CONFIG=/app/.bundle
    depends_on:
      - LB
    ports:
      - :8001
    networks:
      cluster${KEY}-net:
        ipv4_address: 10.0.$((KEY+1)).$((10 + j))
    tty: true
EOW
    done

    cat >> ./cluster$KEY/compose.yml <<EOF

networks:
  cluster${KEY}-net:
    name: cluster${KEY}-local-net
    driver: bridge
    ipam:
      config:
        - subnet: 10.0.$((KEY+1)).0/24
          gateway: 10.0.$((KEY+1)).1

  overlay-net:
    external: true
EOF

    cd ./cluster$KEY
    docker compose -f compose.yml up -d
    cd ../

    json_output
    docker network connect deployment Cluster${KEY}_LB 

    KEY=$((KEY + 1))
done

echo "}" >> $JSON_FILE

# prometheus.yml設定
pwd # /home/enk/docker/master/go_lb/cmd
cd ../
BASE_PATH="prometheus"
FEDERATION_PATH="$BASE_PATH/federation/prometheus.yml"

# federation用のターゲットを保持
FED_TARGETS=()

count=0
container=$(docker ps --filter "name=_LB" --format "{{.Names}}" | head -n 1)
KEY=${container:7:1}

for count in $(seq 0 "$KEY");
do 
  container_name="Cluster${count}_LB"
  IP_Addresses=$(docker exec $container_name hostname -i)
  IP_Array=($IP_Addresses)

  FED_TARGETS+=("\"${IP_Array[2]}:9090\"")
  # echo $IP_Addresses $IP_Array ${IP_Array[0]} ${IP_Array[1]} ${IP_Array[2]} $count
done

echo ${FED_TARGETS[*]}

# federation/prometheus.yml を生成
mkdir -p "$(dirname "$FEDERATION_PATH")"
FED_TARGETS_JOINED=$(IFS=,; echo "${FED_TARGETS[*]}")
cat > "$FEDERATION_PATH" <<EOF
global:
  scrape_interval: 1s

scrape_configs:
  - job_name: 'federate'
    scrape_interval: 1s
    honor_labels: true
    metrics_path: '/federate'
    params:
      'match[]':
        - '{job=~".+"}'
    static_configs:
      - targets: [ $FED_TARGETS_JOINED ]
EOF

echo "federation/prometheus.yml を生成"

# prometheusコンテナ再起動
docker kill -s HUP prometheus-federate