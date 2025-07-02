#!/bin/bash

target=$1
attempts=$2
output="${target}/jmeter.csv"

echo $target $output $attempts

readarray -t jmeter_logs < <(
  find "$target" -type f -name "jmeter_*.log" |
  grep -E 'jmeter_[0-9]+_[0-9]{8}_[0-9]{6}\.log$' |
  sort
)

if [[ ${#jmeter_logs[@]} -eq 0 ]]; then
  echo "No valid jmeter logs found in $target_dir"
else
  echo "Found ${#jmeter_logs[@]} jmeter log files:"
  for log in "${jmeter_logs[@]}"; do
    echo "$log"
  done
fi

echo "total,req/s,average,min,max,err" > $output

for ((i = 1; i <= attempts; i++)); do
  matched_log=""
  
  for log in "${jmeter_logs[@]}"; do
    filename=$(basename "$log")
    if [[ "$filename" =~ ^jmeter_${i}_[0-9]{8}_[0-9]{6}\.log$ ]]; then
      matched_log="$log"
      break
    fi
  done

  if [[ -z "$matched_log" ]]; then
    echo "jmeter_${i}_*.log が見つかりません。スキップします。"
    continue
  fi

  line=$(grep "summary =" "$matched_log" | tail -n 1)

  if [[ -z "$line" ]]; then
    echo "summary 行が $matched_log に見つかりません。スキップします。"
    continue
  fi

  # 各項目抽出
  total=$(echo "$line" | awk -F'summary = | in' '{print $2}' | tr -d ' ')
  reqps=$(echo "$line" | awk -F'= |/s' '{print $(NF-1)}')
  avg=$(echo "$line" | awk -F'Avg: *' '{print $2}' | awk '{print $1}')
  min=$(echo "$line" | awk -F'Min: *' '{print $2}' | awk '{print $1}')
  max=$(echo "$line" | awk -F'Max: *' '{print $2}' | awk '{print $1}')
  err=$(echo "$line" | awk -F'Err: *' '{print $2}' | awk '{print $1}')

  # 出力
  echo "$total,$reqps,$avg,$min,$max,$err" >> "$output"
done

echo "CSV出力完了: $output"