#!/bin/bash

# set propagation delay between links

# 変数の初期化
declare -A cluster_interface_map  # 連想配列: cluster_interface_map[クラスタID]=インターフェース名

# Cluster*_LBの名前を持つコンテナがあれば、*の数を確認
echo "=== Cluster LB コンテナの確認 ==="
cluster_containers=$(docker ps --format "table {{.Names}}" | grep -E "^Cluster[0-9]+_LB$" | sort)

if [ -z "$cluster_containers" ]; then
    echo "Cluster*_LB パターンのコンテナが見つかりませんでした。"
    exit 1
fi

echo "発見されたCluster LBコンテナ:"
echo "$cluster_containers"

# クラスタ数を計算
cluster_count=$(echo "$cluster_containers" | wc -l)
echo "発見されたクラスタ数: $cluster_count"

echo
echo "=== クラスタ間ネットワーク情報 ==="

# 各Cluster LBコンテナのクラスタ間IPアドレスを調べる
echo "$cluster_containers" | while read -r container; do
    if [ -z "$container" ]; then
        continue
    fi
    
    # コンテナが動作中かチェック
    if ! docker ps --format "{{.Names}}" | grep -q "^${container}$"; then
        continue
    fi
    
    # クラスタIDを抽出 (Cluster0_LB → 0)
    cluster_id=$(echo "$container" | sed -n 's/^Cluster\([0-9]\+\)_LB$/\1/p')
    
    # ifconfigから172.で始まるIPアドレスとインターフェース名を抽出
    docker exec "$container" ifconfig 2>/dev/null | awk -v cluster_id="$cluster_id" '
    /^[a-zA-Z0-9]+/ {
        # インターフェース名の行
        interface = $1
        # コロンを削除
        gsub(/:$/, "", interface)
    }
    /inet / && /172\./ {
        # 172.で始まるIPv4アドレスの行
        if ($2 ~ /^addr:/) {
            # 古い形式: inet addr:172.18.4.2
            ip = $2
            gsub(/^addr:/, "", ip)
        } else {
            # 新しい形式: inet 172.18.4.2
            ip = $2
        }
        printf "%s: %s (%s)\n", cluster_id, ip, interface
    }'
done

# クラスタ情報の収集と表示
echo
echo "=== クラスタ情報の収集 ==="
echo "$cluster_containers" | while read -r container; do
    if [ -z "$container" ]; then
        continue
    fi
    
    # コンテナが動作中かチェック
    if ! docker ps --format "{{.Names}}" | grep -q "^${container}$"; then
        continue
    fi
    
    # クラスタIDを抽出
    cluster_id=$(echo "$container" | sed -n 's/^Cluster\([0-9]\+\)_LB$/\1/p')
    
    # ifconfigから172.で始まるインターフェース名を抽出
    interface_name=$(docker exec "$container" ifconfig 2>/dev/null | awk '
    /^[a-zA-Z0-9]+/ {
        interface = $1
        gsub(/:$/, "", interface)
    }
    /inet / && /172\./ {
        printf "%s", interface
        exit
    }')
    
    if [ -n "$interface_name" ]; then
        # 一時ファイルに保存（サブシェルの制限回避）
        echo "$cluster_id $interface_name" >> /tmp/cluster_map.tmp
        echo "$cluster_id: $interface_name"
    fi
done

# 一時ファイルから連想配列に読み込み
if [ -f /tmp/cluster_map.tmp ]; then
    while read -r cluster_id interface_name; do
        cluster_interface_map["$cluster_id"]="$interface_name"
    done < /tmp/cluster_map.tmp
    rm -f /tmp/cluster_map.tmp
fi

# adjacentList.jsonから隣接情報を読み取り
echo
echo "=== 隣接クラスタ情報 ==="

# JSONファイルが存在するかチェック
if [ ! -f "../json/adjacentList.json" ]; then
    echo "エラー: adjacentList.json が見つかりません"
else
    # JSONから隣接情報を抽出
    python3 -c "
import json
import sys

try:
    with open('../json/adjacentList.json', 'r') as f:
        data = json.load(f)
    
    for cluster_name, cluster_info in sorted(data.items()):
        # cluster0 -> 0, cluster1 -> 1 のように変換
        cluster_id = cluster_name.replace('cluster', '')
        adjacent_ids = []
        
        if 'adjacentList' in cluster_info:
            for adj_cluster in cluster_info['adjacentList'].keys():
                adj_id = adj_cluster.replace('cluster', '')
                adjacent_ids.append(adj_id)
        
        # ソートして出力
        adjacent_ids.sort(key=int)
        adjacent_list_str = ', '.join(adjacent_ids)
        print(cluster_id + ': ' + adjacent_list_str)

except Exception as e:
    print('エラー: ' + str(e), file=sys.stderr)
"
fi

# クラスタ間に遅延を設定
