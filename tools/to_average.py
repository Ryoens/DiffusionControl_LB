import os
import re
import argparse
import pandas as pd
import numpy as np
import warnings
from collections import defaultdict

warnings.filterwarnings("ignore", category=RuntimeWarning)

def find_csv_files(directory, cluster_num):
    pattern = re.compile(rf"Cluster{cluster_num}_(\d{{1}}_\d{{8}}_\d{{6}})\.csv")
    cluster_files = []
    
    try:
        for filename in os.listdir(directory):
            match = pattern.match(filename)
            if match:
                full_path = os.path.join(directory, filename)
                cluster_files.append(full_path)
    except FileNotFoundError:
        print(f"Error: Specified directory '{directory}' not found.")
        return []
    
    return sorted(cluster_files)

def remove_empty_rows_per_file(csv_file):
    df = pd.read_csv(csv_file)
    df_numeric = df.apply(pd.to_numeric, errors='coerce')
    df_cleaned = df.loc[~(df.iloc[:, 1:].eq(0).all(axis=1))]
    df_cleaned.reset_index(drop=True, inplace=True)

    # -------------------
    # drop_index = None
    # for i in range(1, len(df_cleaned)):
    #     if df_cleaned.iloc[i].equals(df_cleaned.iloc[i - 1]):
    #         drop_index = i
    #         break

    # if drop_index is not None:
    #     df_cleaned = df_cleaned.iloc[:drop_index]
    #     # print(f"Detected two consecutive rows with the same values. Rows from {drop_index} onwards have been removed.")
    # -------------------

    directory, base_name = os.path.split(csv_file)
    output_file = os.path.join(directory, f"cleaned_{base_name}")
    df_cleaned.to_csv(output_file, index=False)
    # print(f"Created {output_file} (rows: {len(df_cleaned)})")
    
    return output_file

def process_all_clusters(input_dir, avg_output_file, index):
    selected_columns = ["TotalQueue", "Queue", "CurrentResponse"]
    
    all_data = {cluster: [] for cluster in range(index)}
    for cluster_num in range(index): 
        file_paths = find_csv_files(input_dir, cluster_num)
        for file in file_paths:
            cleaned_file = remove_empty_rows_per_file(file)
            df = pd.read_csv(cleaned_file)
            if all(col in df.columns for col in selected_columns):
                df_selected = df[selected_columns].apply(pd.to_numeric, errors='coerce')
                all_data[cluster_num].append(df_selected)
                
    max_length = max(
        max(len(df) for df in cluster_data) if cluster_data else 0
        for cluster_data in all_data.values()
    )
    for cluster_num in range(index):
        for i in range(len(all_data[cluster_num])):
            df = all_data[cluster_num][i][selected_columns]
            if len(df) < max_length:
                padding = pd.DataFrame(np.nan, index=range(max_length - len(df)), columns=selected_columns)
                all_data[cluster_num][i] = pd.concat([df, padding], ignore_index=True)
            else:
                all_data[cluster_num][i] = df

    avg_results = []
    for row in range(max_length):
        avg_row = []
        skip_row = False  # ← Set to True if NaN is found

        for cluster_num in range(index):
            if all_data[cluster_num]:
                stacked_data = np.stack([
                    df.iloc[row].to_numpy()
                    for df in all_data[cluster_num]
                    if row < len(df)
                ])
                if stacked_data.size > 0:
                    stacked_data = stacked_data.astype(np.float64)
                    avg_value = np.nanmean(stacked_data, axis=0)

                    # If even one NaN is found, stop
                    if np.isnan(avg_value).any():
                        skip_row = True
                        break

                    avg_row.extend([int(round(val)) for val in avg_value])
                else:
                    skip_row = True
                    break
            else:
                skip_row = True
                break

        if skip_row:
            break  # Skip the rest of the rows and end the entire loop

        avg_results.append(avg_row)

    columns = []
    for cluster_num in range(index):
        for col in selected_columns:
            columns.append(f"Cluster{cluster_num}_{col}")
            
    # Save the average DataFrame
    avg_df = pd.DataFrame(avg_results, columns=columns)
    avg_df.to_csv(avg_output_file, index=False)
    
    print(f"Average data for all clusters has been output to {avg_output_file}.")

def main():
    parser = argparse.ArgumentParser(description="Process all cluster CSVs and calculate averages")
    parser.add_argument("directory", type=str, help="Specify the directory containing CSV files")
    parser.add_argument("cluster_index", type=int, help="Specify the cluster index (used as +1 in processing)")
    args = parser.parse_args()

    adjusted_index = args.cluster_index + 1
    
    avg_output_csv = os.path.join(args.directory, "average_all_clusters.csv")
    process_all_clusters(args.directory, avg_output_csv, adjusted_index)

if __name__ == "__main__":
    main()