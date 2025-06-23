import sys
import json
import networkx as nx
import matplotlib.pyplot as plt

args = sys.argv
print(args[1])

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

# 可視化
nx.draw_networkx(g)
plt.show()

# adjacentList.jsonの出力（クラスタ名＋IPに変換）
adjacent_output = {}

for node_index, neighbors in adj_list.items():
    cluster_name = index_to_cluster[node_index]
    adjacent_list = {
        index_to_cluster[n]: cluster_data[index_to_cluster[n]]["cluster_lb"]
        for n in neighbors
    }
    internal_list = cluster_data[cluster_name]
    adjacent_output[cluster_name] = {
        "adjacentList": adjacent_list,
        "internalList": internal_list
    }

with open("../json/adjacentList.json", "w") as f:
    json.dump(adjacent_output, f, indent=2)

print("adjacentList.json を保存しました")