#!/bin/bash

# Execute.shから実行する
# curlで大量にリクエストを送信
duration=60
vus=100
start_time=$(date +%s)
for (( i=0; i<vus; i++ )); do 
    (
        while true;
        do
            current_time=$(date +%s)

            if (( current_time - start_time >= duration)); then
                exit(1)
            fi

            curl -X GET "http://114.51.4.2:8001"
            count=$(expr $count + 1)
        done
    ) &
done
