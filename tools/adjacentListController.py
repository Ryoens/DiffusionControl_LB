import sys
import json
import networkx as nx
import numpy as np
import matplotlib.pyplot as plt

args = sys.argv
print(args[1])

if len(args) == 2:
    # Only augument is nw_model
    nw_model = args[1]
    print("Default start: No cluster/web reductions")
elif (len(args) - 2) % 2 != 0:
    # Exception handling
    print("Usage: python3 adjacentListController.py <nw_model> <cls0> <cls1> ... <web0> <web1> ...")
    print("Error: You must provide pairs of <cls> and <web> values.")
else:
    # When arguments include nw_model, cls, and web
    nw_model = args[1]
    raw_values = args[2:]
    half = len(raw_values) // 2
    cluster = [int(x) for x in raw_values[:half]]
    web = [int(x) for x in raw_values[half:]]
    print(cluster, web)

print("Network model:", nw_model)

# Read JSON file
with open("../json/config.json") as f:
    cluster_data = json.load(f)

cluster_names = list(cluster_data.keys())
cluster_count = len(cluster_names)

print(f"Number of clusters: {cluster_count}")
print(f"Cluster list: {cluster_names}")

# Mapping between node names and indices
cluster_to_index = {name: i for i, name in enumerate(cluster_names)}
index_to_cluster = {i: name for i, name in enumerate(cluster_names)}

# Graph construction
if args[1] == "r":
    p = 0.3
    seed = 3
    g = nx.fast_gnp_random_graph(cluster_count, p, seed)
    print("Graph info: Random graph", g)

elif args[1] == "ba":
    seed = 0
    g = nx.barabasi_albert_graph(cluster_count, 3, seed)
    print("Graph info: BA graph", g)

elif args[1] == "f":
    g = nx.complete_graph(cluster_count)
    print("Graph info: Full mesh", g)
else:
    print("Invalid argument. Please specify 'r', 'ba', or 'fullmesh'.")
    exit(1)

# Generate adjacency list
print("Adjacency list")
adj_list = nx.to_dict_of_lists(g)
print(adj_list)

# Graph drawing
pos = {
        n: (np.cos(2*i*np.pi/cluster_count), np.sin(2*i*np.pi/cluster_count))
        for i, n in enumerate(g.nodes)
    }

# Visualization
nx.draw_networkx(g, pos, node_color='skyblue')
plt.show()
filename = f"../data/figure_{args[1]}.png"
plt.savefig(filename)

# Output adjacentList.json (convert cluster names + IP)
adjacent_output = {}

cluster_web_limit = {
    f"cluster{cluster[i]}": web[i]
    for i in range(len(cluster))
} if 'cluster' in locals() else {}

for node_index, neighbors in adj_list.items():
    cluster_name = index_to_cluster[node_index]
    original_data = cluster_data[cluster_name]

    # Map adjacent clusters to IPs
    adjacent_list = {
        index_to_cluster[n]: cluster_data[index_to_cluster[n]]["cluster_lb"]
        for n in neighbors
    }

    # Generate internalList
    internal_list = {
        "cluster_lb": original_data["cluster_lb"]
    }

    # Control the number of web servers according to the specified limit
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

print("Saved adjacentList.json")