#!/bin/bash

KEY=0
QTY_CLUSTER=$(($1))

docker stop redis-server 
# prometheus-federate grafana
docker rm redis-server 
# prometheus-federate grafana

echo $QTY_CLUSTER

while [ $KEY -le $QTY_CLUSTER ]
do
	cd ./cluster$KEY
	docker compose down
	cd ../
	rm -rf ./cluster$KEY

	KEY=`expr $KEY + 1`
done

docker network rm overlay-net
# deployment 