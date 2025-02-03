import os
import re
import argparse
import pandas as pd
import numpy as np
from collections import defaultdict

def find_csv_files(directory, cluster_num):
    pattern = re.compile(rf"Cluster{cluster_num}_(\d{{8}}_\d{{6}})\.csv")
    cluster_files = []
    
    try:
        for filename in os.listdir(directory):
            match = pattern.match(filename)
            if match:
                full_path = os.path.join(directory, filename)
                cluster_files.append(full_path)
    except FileNotFoundError:
        print(f"エラー: 指定されたディレクトリ '{directory}' が見つかりません。")
        return []
    
    return sorted(cluster_files)

def remove_empty_rows_per_file(csv_file):
    df = pd.read_csv(csv_file)
    df_numeric = df.apply(pd.to_numeric, errors='coerce')
    df_cleaned = df.loc[~(df.iloc[:, 1:].eq(0).all(axis=1))]
    df_cleaned.reset_index(drop=True, inplace=True)
    
    directory, base_name = os.path.split(csv_file)
    output_file = os.path.join(directory, f"cleaned_{base_name}")
    df_cleaned.to_csv(output_file, index=False)
    print(f"{output_file} を作成しました！（行数: {len(df_cleaned)}）")
    
    return output_file

def process_all_clusters(input_dir, median_output_file):
    selected_columns = ["TotalQueue", "CurrentQueue", "CurrentResponse"]
    
    all_data = {cluster: [] for cluster in range(5)}
    for cluster_num in range(5):  # Cluster0 から Cluster4 まで
        file_paths = find_csv_files(input_dir, cluster_num)
        for file in file_paths:
            cleaned_file = remove_empty_rows_per_file(file)
            df = pd.read_csv(cleaned_file)
            if all(col in df.columns for col in selected_columns):
                all_data[cluster_num].append(df[selected_columns])
    
    max_length = max(max(len(df) for df in cluster_data) if cluster_data else 0 for cluster_data in all_data.values())
    for cluster_num in range(5):
        for i in range(len(all_data[cluster_num])):
            if len(all_data[cluster_num][i]) < max_length:
                padding = pd.DataFrame(np.nan, index=range(max_length - len(all_data[cluster_num][i])), columns=selected_columns)
                all_data[cluster_num][i] = pd.concat([all_data[cluster_num][i], padding], ignore_index=True)
    
    median_results = []
    for row in range(max_length):
        median_row = []
        for cluster_num in range(5):
            if all_data[cluster_num]:
                stacked_data = np.stack([df.iloc[row].to_numpy() for df in all_data[cluster_num] if row < len(df)])
                if stacked_data.size > 0:
                    median_row.extend(np.nanmedian(stacked_data, axis=0))
                else:
                    median_row.extend([np.nan] * 3)
            else:
                median_row.extend([np.nan] * 3)
        median_results.append(median_row)
    
    columns = [
        "Cluster0_TotalQueue_Median", "Cluster1_TotalQueue_Median", "Cluster2_TotalQueue_Median", "Cluster3_TotalQueue_Median", "Cluster4_TotalQueue_Median", 
        "Cluster0_CurrentQueue_Median", "Cluster1_CurrentQueue_Median", "Cluster2_CurrentQueue_Median", "Cluster3_CurrentQueue_Median", "Cluster4_CurrentQueue_Median", 
        "Cluster0_CurrentResponse_Median", "Cluster1_CurrentResponse_Median", "Cluster2_CurrentResponse_Median", "Cluster3_CurrentResponse_Median", "Cluster4_CurrentResponse_Median"
    ]
    
    median_df = pd.DataFrame(median_results, columns=columns)
    median_df.to_csv(median_output_file, index=False)
    
    print(f"全クラスタの中央値データを {median_output_file} に出力しました。")

def main():
    parser = argparse.ArgumentParser(description="全クラスタのCSVを処理し、中央値を算出")
    parser.add_argument("directory", type=str, help="CSVファイルが存在するディレクトリを指定")
    args = parser.parse_args()
    
    median_output_csv = os.path.join(args.directory, "median_all_clusters.csv")
    process_all_clusters(args.directory, median_output_csv)

if __name__ == "__main__":
    main()