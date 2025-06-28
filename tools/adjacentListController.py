import sys
import json
import networkx as nx
import numpy as np
import matplotlib.pyplot as plt

args = sys.argv
print(args[1])

if len(args) == 2:
    # 引数がnw_modelのみ
    nw_model = args[1]
    print("Default start: No cluster/web reductions")
elif (len(args) - 2) % 2 != 0:
    # 例外処理
    print("Usage: python3 adjacentListController.py <nw_model> <cls0> <cls1> ... <web0> <web1> ...")
    print("Error: You must provide pairs of <cls> and <web> values.")
else:
    # 引数にnw_model, cls, webがある場合
    nw_model = args[1]
    raw_values = args[2:]
    half = len(raw_values) // 2
    cluster = [int(x) for x in raw_values[:half]]
    web = [int(x) for x in raw_values[half:]]
    print(cluster, web)

print("Network model:", nw_model)

# JSONファイル読み込み
with open("../json/config.json") as f:
    cluster_data = json.load(f)

cluster_names = list(cluster_data.keys())
cluster_count = len(cluster_names)

print(f"クラスタ数: {cluster_count}")
print(f"クラスタ一覧: {cluster_names}")

# ノード名とインデックスの対応
cluster_to_index = {name: i for i, name in enumerate(cluster_names)}
index_to_cluster = {i: name for i, name in enumerate(cluster_names)}

# for debug
# cluster_count = 20

# グラフ構築
if args[1] == "r":
    p = 0.3
    seed = 3
    g = nx.fast_gnp_random_graph(cluster_count, p, seed)
    print("グラフの情報: ランダムグラフ", g)

elif args[1] == "ba":
    seed = 0
    g = nx.barabasi_albert_graph(cluster_count, 3, seed)
    print("グラフの情報: BAグラフ", g)

elif args[1] == "f":
    g = nx.complete_graph(cluster_count)
    print("グラフの情報: フルメッシュ", g)

else:
    print("無効な引数です。'r', 'ba', 'fullmesh' を指定してください")
    exit(1)

# 隣接リスト生成
print("隣接リスト")
adj_list = nx.to_dict_of_lists(g)
print(adj_list)

# グラフ描画
pos = {
        n: (np.cos(2*i*np.pi/cluster_count), np.sin(2*i*np.pi/cluster_count))
        for i, n in enumerate(g.nodes)
    }

# 可視化
nx.draw_networkx(g, pos, node_color='skyblue')
plt.show()
filename = f"../../data/figure_{args[1]}.png"
plt.savefig(filename)

# adjacentList.jsonの出力（クラスタ名＋IPに変換）
adjacent_output = {}

cluster_web_limit = {
    f"cluster{cluster[i]}": web[i]
    for i in range(len(cluster))
} if 'cluster' in locals() else {}

for node_index, neighbors in adj_list.items():
    cluster_name = index_to_cluster[node_index]
    original_data = cluster_data[cluster_name]

    # 隣接クラスタのリストをIPでマッピング
    adjacent_list = {
        index_to_cluster[n]: cluster_data[index_to_cluster[n]]["cluster_lb"]
        for n in neighbors
    }

    # internalListの生成
    internal_list = {
        "cluster_lb": original_data["cluster_lb"]
    }

    # 指定された制限に従って web サーバ数を制御
    web_keys = sorted([k for k in original_data if k.startswith("web")])
    limit = cluster_web_limit.get(cluster_name, len(web_keys))

    for wk in web_keys[:limit]:
        internal_list[wk] = original_data[wk]

    adjacent_output[cluster_name] = {
        "adjacentList": adjacent_list,
        "internalList": internal_list
    }

with open("../json/adjacentList.json", "w") as f:
    json.dump(adjacent_output, f, indent=2)

print("adjacentList.json を保存しました")