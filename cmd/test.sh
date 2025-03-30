#!/bin/bash

# ユーザー入力
read -p "テスト対象URL: " url
read -p "並列ユーザー数 (VUS): " vus
read -p "実験時間 (秒): " duration

# 総リクエスト数を計算 (並列ユーザー数 × 実験時間)
total_requests=$((vus * duration))
echo "総リクエスト数: $total_requests"

# 結果保存ファイル
timestamp=$(date +"%Y%m%d_%H%M%S")
result_file="load_test_result_${timestamp}.csv"

# ヘッダーを書き込み
echo "RequestID,ResponseTime(ms),HTTPStatus" > "$result_file"

# 負荷テストの実行
echo "負荷テスト開始: $vus ユーザー並列で $duration 秒間リクエストを送信"

start_time=$(date +%s)
request_count=0

while [ $(($(date +%s) - start_time)) -lt "$duration" ]; do
    for ((i=0; i<vus; i++)); do
        (
            # 各リクエストの応答時間を測定
            response=$(curl -o /dev/null -s -w "%{time_total},%{http_code}\n" "$url")
            echo "$request_count,$response" >> "$result_file"
        ) &
        ((request_count++))
    done
    sleep 1  # 1秒ごとにリクエスト
done

wait  # 全プロセスが完了するまで待つ

echo "負荷テスト完了。結果は $result_file に保存しました。"
