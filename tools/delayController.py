#!/usr/bin/env python3
"""
delayController.py - クラスタ間リンク遅延制御ツール

Docker コンテナ (Cluster*_LB) のネットワーク情報を収集し,
隣接クラスタ情報を行列形式で表示します.
"""

import os
import sys
import json
import subprocess
import re
from typing import Dict, List, Set, Tuple, Optional


class ClusterInfo:
    """クラスタ情報を管理するクラス"""
    
    def __init__(self):
        self.containers: List[str] = []
        self.cluster_count: int = 0
        self.interface_map: Dict[str, str] = {}  # {cluster_id: interface_name}
        self.ip_map: Dict[str, str] = {}  # {cluster_id: ip_address}
        self.adjacency: Dict[str, Set[str]] = {}  # {cluster_id: set(adjacent_ids)}
        self.adjacency_ips: Dict[str, Dict[str, str]] = {}  # {cluster_id: {adjacent_id: ip}}
    
    def discover_containers(self) -> bool:
        """Cluster*_LB パターンのコンテナを発見"""
        try:
            result = subprocess.run(
                ['docker', 'ps', '--format', '{{.Names}}'],
                capture_output=True,
                text=True,
                check=True
            )
            
            # Cluster*_LB パターンでフィルタ
            pattern = re.compile(r'^Cluster\d+_LB$')
            self.containers = sorted([
                line for line in result.stdout.strip().split('\n')
                if pattern.match(line)
            ])
            
            if not self.containers:
                return False
            
            self.cluster_count = len(self.containers)
            return True
            
        except subprocess.CalledProcessError as e:
            print(f"エラー: docker ps の実行に失敗しました: {e}", file=sys.stderr)
            return False
    
    def extract_cluster_id(self, container_name: str) -> Optional[str]:
        """コンテナ名からクラスタIDを抽出 (Cluster0_LB -> 0)"""
        match = re.match(r'Cluster(\d+)_LB', container_name)
        return match.group(1) if match else None
    
    def get_interface_info(self, container: str) -> Optional[Tuple[str, str]]:
        """コンテナから172.x.x.xのインターフェース名とIPアドレスを取得"""
        try:
            result = subprocess.run(
                ['docker', 'exec', container, 'ifconfig'],
                capture_output=True,
                text=True,
                check=True
            )
            
            lines = result.stdout.split('\n')
            current_interface = None
            
            for line in lines:
                # インターフェース名の行
                if line and not line.startswith(' '):
                    parts = line.split()
                    if parts:
                        current_interface = parts[0].rstrip(':')
                
                # inet行で172.で始まるIPを探す
                if 'inet ' in line and '172.' in line:
                    # 古い形式: inet addr:172.18.4.2 または 新しい形式: inet 172.18.4.2
                    match = re.search(r'inet (?:addr:)?(\d+\.\d+\.\d+\.\d+)', line)
                    if match:
                        ip = match.group(1)
                        if ip.startswith('172.'):
                            return (current_interface, ip.split('/')[0])
            
            return None
            
        except subprocess.CalledProcessError:
            return None
    
    def collect_network_info(self):
        """各コンテナのネットワーク情報を収集"""
        for container in self.containers:
            cluster_id = self.extract_cluster_id(container)
            if not cluster_id:
                continue
            
            info = self.get_interface_info(container)
            if info:
                interface_name, ip_address = info
                self.interface_map[cluster_id] = interface_name
                self.ip_map[cluster_id] = ip_address
    
    def load_adjacency_info(self, json_path: str = "../json/adjacentList.json") -> bool:
        """adjacentList.jsonから隣接情報を読み込み"""
        if not os.path.exists(json_path):
            print(f"エラー: {json_path} が見つかりません")
            return False
        
        try:
            with open(json_path, 'r') as f:
                data = json.load(f)
            
            # 隣接情報を構築
            for cluster_name in sorted(data.keys()):
                cluster_id = cluster_name.replace('cluster', '')
                self.adjacency[cluster_id] = set()
                self.adjacency_ips[cluster_id] = {}
                
                if 'adjacentList' in data[cluster_name]:
                    for adj_cluster, adj_ip in data[cluster_name]['adjacentList'].items():
                        adj_id = adj_cluster.replace('cluster', '')
                        self.adjacency[cluster_id].add(adj_id)
                        self.adjacency_ips[cluster_id][adj_id] = adj_ip
            
            return True
            
        except Exception as e:
            print(f"エラー: {e}", file=sys.stderr)
            return False
    
    def _print_adjacency_matrix(self, cluster_ids: List[str]):
        """隣接情報を行列形式で出力"""
        # ヘッダー行
        header = "    " + " ".join([f"{cid:>3}" for cid in cluster_ids])
        print(header)
        print("    " + "-" * (4 * len(cluster_ids)))
        
        # 各行
        for src_id in cluster_ids:
            row = f"{src_id:>3} |"
            for dst_id in cluster_ids:
                if src_id == dst_id:
                    row += "  - "
                elif dst_id in self.adjacency.get(src_id, set()):
                    row += "  O "
                else:
                    row += "  . "
            print(row)
    
    def print_container_info(self):
        """コンテナ情報を表示"""
        print("=== Cluster LB コンテナの確認 ===")
        if not self.containers:
            print("Cluster*_LB パターンのコンテナが見つかりませんでした.")
            return
        
        print("発見されたCluster LBコンテナ:")
        for container in self.containers:
            print(f"  {container}")
        print(f"発見されたクラスタ数: {self.cluster_count}")
    
    def print_network_info(self):
        """ネットワーク情報を表示"""
        print("\n=== クラスタ間ネットワーク情報 ===")
        for cluster_id in sorted(self.ip_map.keys(), key=int):
            ip = self.ip_map[cluster_id]
            interface = self.interface_map.get(cluster_id, "不明")
            print(f"{cluster_id}: {ip} ({interface})")
        
        print("\n=== クラスタ情報の収集 ===")
        for cluster_id in sorted(self.interface_map.keys(), key=int):
            print(f"{cluster_id}: {self.interface_map[cluster_id]}")
    
    def print_adjacency_matrix(self):
        """隣接情報を行列形式で表示"""
        print("\n=== 隣接クラスタ情報（行列形式） ===")
        cluster_ids = sorted(self.adjacency.keys(), key=int)
        self._print_adjacency_matrix(cluster_ids)
    
    def get_all_links(self) -> List[Tuple[str, str]]:
        """全てのリンクのリストを取得（重複なし）"""
        links = []
        processed = set()
        
        for src_id in sorted(self.adjacency.keys(), key=int):
            for dst_id in self.adjacency[src_id]:
                # 双方向リンクの重複を避ける（小さいIDを先に）
                link_pair = tuple(sorted([src_id, dst_id], key=int))
                if link_pair not in processed:
                    processed.add(link_pair)
                    links.append(link_pair)
        
        return links
    
    def prompt_delay_configuration(self) -> Tuple[int, List[Tuple[str, str]], Dict[Tuple[str, str], int]]:
        """
        ユーザーに遅延設定を尋ねる
        
        Returns:
            (選択タイプ, 選択されたリンクリスト, 遅延設定辞書 {(src, dst): delay_ms})
        """
        print("\n=== リンク遅延設定 ===")
        print("遅延を設定する対象を選択してください:")
        print("  1. 全リンクに対して遅延設定")
        print("  2. 一部のリンクに対して手動で遅延設定")
        print("  3. 既存の遅延設定を削除")
        print("  4. 遅延なし(終了)")
        
        while True:
            try:
                choice = input("\n選択 (1-4): ").strip()
                if choice not in ['1', '2', '3', '4']:
                    print("エラー: 1, 2, 3, 4 のいずれかを入力してください")
                    continue
                
                choice_num = int(choice)
                
                # 4. 遅延なし
                if choice_num == 4:
                    print("遅延設定をスキップします.")
                    return (4, [], {})
                
                # 3. 遅延削除
                if choice_num == 3:
                    return (3, [], {})
                
                # 1. 全リンク
                if choice_num == 1:
                    all_links = self.get_all_links()
                    delay_config = self._configure_all_links_delay(all_links)
                    if delay_config:
                        return (1, all_links, delay_config)
                    else:
                        continue
                
                # 2. 一部のリンク(手動選択)
                if choice_num == 2:
                    selected_links = self._select_specific_links()
                    if selected_links:
                        delay_ms = self._get_delay_input()
                        delay_config = {link: delay_ms for link in selected_links}
                        print(f"\n選択された {len(selected_links)} リンクに {delay_ms}ms の遅延を設定します.")
                        return (2, selected_links, delay_config)
                    else:
                        print("リンクが選択されませんでした.")
                        continue
                        
            except KeyboardInterrupt:
                print("\n\n中断されました.")
                sys.exit(0)
            except Exception as e:
                print(f"エラー: {e}")
                continue
    
    def _configure_all_links_delay(self, all_links: List[Tuple[str, str]]) -> Optional[Dict[Tuple[str, str], int]]:
        """
        全リンクに対する遅延設定方法を選択
        
        Returns:
            遅延設定辞書 {(src, dst): delay_ms} または None
        """
        print(f"\n全 {len(all_links)} リンクの遅延設定方法を選択してください:")
        print("  1. 手動で同じ遅延を設定")
        print("  2. ランダムに遅延を割り当て")
        
        while True:
            try:
                method = input("\n選択 (1-2): ").strip()
                if method not in ['1', '2']:
                    print("エラー: 1 または 2 を入力してください")
                    continue
                
                if method == '1':
                    # 手動で同じ遅延
                    delay_ms = self._get_delay_input()
                    delay_config = {link: delay_ms for link in all_links}
                    print(f"\n全 {len(all_links)} リンクに {delay_ms}ms の遅延を設定します.")
                    return delay_config
                
                else:
                    # ランダムに遅延を割り当て
                    delay_config = self._assign_random_delays(all_links)
                    if delay_config:
                        return delay_config
                    else:
                        continue
                        
            except Exception as e:
                print(f"エラー: {e}")
                continue
    
    def _assign_random_delays(self, links: List[Tuple[str, str]]) -> Optional[Dict[Tuple[str, str], int]]:
        """
        リンクにランダムな遅延を割り当て
        
        Returns:
            遅延設定辞書 {(src, dst): delay_ms} または None
        """
        import random
        
        print("\nランダム遅延の設定:")
        
        # 遅延範囲を入力
        while True:
            try:
                min_delay_input = input("最小遅延 (ms) [デフォルト: 10]: ").strip()
                min_delay = int(min_delay_input) if min_delay_input else 10
                
                max_delay_input = input("最大遅延 (ms) [デフォルト: 100]: ").strip()
                max_delay = int(max_delay_input) if max_delay_input else 100
                
                if min_delay < 0 or max_delay < 0:
                    print("エラー: 正の整数を入力してください")
                    continue
                
                if min_delay > max_delay:
                    print("エラー: 最小遅延は最大遅延以下である必要があります")
                    continue
                
                break
                
            except ValueError:
                print("エラー: 数値を入力してください")
        
        # ランダムに遅延を割り当て
        delay_config = {}
        for link in links:
            delay_config[link] = random.randint(min_delay, max_delay)
        
        # サンプル表示
        print(f"\nランダムに割り当てられた遅延 (サンプル、最初の5リンク):")
        for i, (link, delay) in enumerate(list(delay_config.items())[:5], 1):
            src, dst = link
            print(f"  {i}. クラスタ {src} - クラスタ {dst}: {delay}ms")
        
        if len(links) > 5:
            print(f"  ... 他 {len(links) - 5} リンク")
        
        confirm = input("\nこの設定でよろしいですか？ (y/n): ").strip().lower()
        if confirm in ['y', 'yes']:
            return delay_config
        else:
            return None
    
    def _get_delay_input(self) -> int:
        """遅延時間の入力を取得"""
        while True:
            try:
                delay_input = input("遅延時間 (ms) [デフォルト: 10]: ").strip()
                if not delay_input:
                    return 10
                delay_ms = int(delay_input)
                if delay_ms < 0:
                    print("エラー: 正の整数を入力してください")
                    continue
                return delay_ms
            except ValueError:
                print("エラー: 数値を入力してください")
    
    def _select_specific_links(self) -> List[Tuple[str, str]]:
        """特定のリンクを選択"""
        all_links = self.get_all_links()
        
        print("\n利用可能なリンク:")
        for i, (src, dst) in enumerate(all_links, 1):
            print(f"  {i}. クラスタ {src} - クラスタ {dst}")
        
        print("\n選択方法:")
        print("  - 単一: 1")
        print("  - 複数: 1,3,5")
        print("  - 範囲: 1-5")
        print("  - 混合: 1,3-5,7")
        
        while True:
            try:
                selection = input("\nリンク番号を入力: ").strip()
                if not selection:
                    print("エラー: 入力が空です")
                    continue
                
                selected_indices = self._parse_selection(selection, len(all_links))
                selected_links = [all_links[i-1] for i in selected_indices]
                
                # 確認表示
                print("\n選択されたリンク:")
                for src, dst in selected_links:
                    print(f"  - クラスタ {src} - クラスタ {dst}")
                
                confirm = input("\nこれでよろしいですか？ (y/n): ").strip().lower()
                if confirm in ['y', 'yes']:
                    return selected_links
                
            except ValueError as e:
                print(f"エラー: {e}")
    

    def _parse_selection(self, selection: str, max_num: int) -> List[int]:
        """選択文字列をパース (例: "1,3-5,7" -> [1,3,4,5,7])"""
        indices = set()
        
        for part in selection.split(','):
            part = part.strip()
            
            # 範囲指定 (例: 3-5)
            if '-' in part:
                try:
                    start, end = part.split('-')
                    start, end = int(start.strip()), int(end.strip())
                    if start < 1 or end > max_num or start > end:
                        raise ValueError(f"範囲 {start}-{end} が無効です (1-{max_num} の範囲で指定)")
                    indices.update(range(start, end + 1))
                except ValueError as e:
                    raise ValueError(f"範囲指定が無効です: {part}")
            
            # 単一指定 (例: 3)
            else:
                try:
                    num = int(part)
                    if num < 1 or num > max_num:
                        raise ValueError(f"番号 {num} が無効です (1-{max_num} の範囲で指定)")
                    indices.add(num)
                except ValueError:
                    raise ValueError(f"無効な番号: {part}")
        
        return sorted(indices)
    
    def apply_delay(self, delay_config: Dict[Tuple[str, str], int]) -> bool:
        """
        tcコマンドを使用してリンクに遅延を設定
        
        Args:
            delay_config: {(src_id, dst_id): delay_ms} の辞書
        
        Returns:
            成功した場合True
        """
        print("\n=== 遅延設定の適用 ===")
        
        success_count = 0
        fail_count = 0
        
        for (src_id, dst_id), delay_ms in delay_config.items():
            # 双方向に設定
            if self._apply_delay_single_direction(src_id, dst_id, delay_ms):
                success_count += 1
            else:
                fail_count += 1
            
            if self._apply_delay_single_direction(dst_id, src_id, delay_ms):
                success_count += 1
            else:
                fail_count += 1
        
        print(f"\n設定完了: 成功 {success_count}, 失敗 {fail_count}")
        return fail_count == 0
    
    def _apply_delay_single_direction(self, src_id: str, dst_id: str, delay_ms: int) -> bool:
        """
        単一方向の遅延を設定
        
        Args:
            src_id: 送信元クラスタID
            dst_id: 宛先クラスタID
            delay_ms: 遅延時間(ミリ秒)
        
        Returns:
            成功した場合True
        """
        # コンテナ名を取得
        container = f"Cluster{src_id}_LB"
        
        # インターフェース名を取得
        interface = self.interface_map.get(src_id)
        if not interface:
            print(f"エラー: クラスタ {src_id} のインターフェースが見つかりません")
            return False
        
        # 宛先IPアドレスを取得
        dst_ip = self.adjacency_ips.get(src_id, {}).get(dst_id)
        if not dst_ip:
            print(f"エラー: クラスタ {src_id} -> {dst_id} の宛先IPが見つかりません")
            return False
        
        try:
            # tcコマンドが利用可能か確認
            check_result = subprocess.run(
                ['docker', 'exec', container, 'sh', '-c', 'command -v tc'],
                capture_output=True,
                text=True
            )
            
            if check_result.returncode != 0:
                print(f"警告: {container} でtcコマンドが利用できません")
                return False
            
            # 1. qdisc設定を確認し、必要に応じて初期化
            check_qdisc = subprocess.run(
                ['docker', 'exec', container, 'tc', 'qdisc', 'show', 'dev', interface],
                capture_output=True,
                text=True
            )
            
            # prioがなければ追加
            if 'prio' not in check_qdisc.stdout:
                subprocess.run(
                    ['docker', 'exec', container, 'tc', 'qdisc', 'add', 'dev', interface,
                     'root', 'handle', '1:', 'prio', 'bands', '3'],
                    check=True,
                    capture_output=True
                )
                print(f"  {container} ({interface}): prio qdisc 設定完了")
            
            # 2. netem qdiscを設定
            subprocess.run(
                ['docker', 'exec', container, 'tc', 'qdisc', 'replace', 'dev', interface,
                 'parent', '1:3', 'handle', '30:', 'netem', 'delay', f'{delay_ms}ms'],
                check=True,
                capture_output=True
            )
            
            # 3. u32フィルタで宛先IPに応じてトラフィックを振り分け
            subprocess.run(
                ['docker', 'exec', container, 'tc', 'filter', 'add', 'dev', interface,
                 'protocol', 'ip', 'parent', '1:0', 'prio', '1', 'u32',
                 'match', 'ip', 'dst', f'{dst_ip}/32', 'flowid', '1:3'],
                check=True,
                capture_output=True
            )
            
            print(f"  {container} -> {dst_ip} ({dst_id}): {delay_ms}ms 遅延設定完了")
            return True
            
        except subprocess.CalledProcessError as e:
            print(f"エラー: {container} での tc コマンド実行に失敗: {e}")
            if e.stderr:
                print(f"  詳細: {e.stderr.decode() if isinstance(e.stderr, bytes) else e.stderr}")
            return False
        except Exception as e:
            print(f"エラー: {container} での遅延設定に失敗: {e}")
            return False
    
    def remove_delay(self) -> bool:
        """
        全てのコンテナから遅延設定を削除
        
        Returns:
            成功した場合True
        """
        print("\n=== 遅延設定の削除 ===")
        
        success_count = 0
        fail_count = 0
        
        for cluster_id in sorted(self.interface_map.keys(), key=int):
            if self._remove_delay_single_container(cluster_id):
                success_count += 1
            else:
                fail_count += 1
        
        print(f"\n削除完了: 成功 {success_count}, 失敗 {fail_count}")
        return fail_count == 0
    
    def _remove_delay_single_container(self, cluster_id: str) -> bool:
        """
        単一コンテナから遅延設定を削除
        
        Args:
            cluster_id: クラスタID
        
        Returns:
            成功した場合True
        """
        container = f"Cluster{cluster_id}_LB"
        interface = self.interface_map.get(cluster_id)
        
        if not interface:
            print(f"エラー: クラスタ {cluster_id} のインターフェースが見つかりません")
            return False
        
        try:
            # tcコマンドが利用可能か確認
            check_result = subprocess.run(
                ['docker', 'exec', container, 'sh', '-c', 'command -v tc'],
                capture_output=True,
                text=True
            )
            
            if check_result.returncode != 0:
                print(f"警告: {container} でtcコマンドが利用できません")
                return False
            
            # qdisc設定を確認
            check_qdisc = subprocess.run(
                ['docker', 'exec', container, 'tc', 'qdisc', 'show', 'dev', interface],
                capture_output=True,
                text=True
            )
            
            # qdisc設定がない場合はスキップ
            if 'qdisc prio' not in check_qdisc.stdout and 'qdisc noqueue' in check_qdisc.stdout:
                print(f"  {container} ({interface}): 遅延設定なし(スキップ)")
                return True
            
            # root qdiscを削除(全てのフィルタとネストされたqdiscも削除される)
            subprocess.run(
                ['docker', 'exec', container, 'tc', 'qdisc', 'del', 'dev', interface, 'root'],
                check=True,
                capture_output=True
            )
            
            print(f"  {container} ({interface}): 遅延設定を削除しました")
            return True
            
        except subprocess.CalledProcessError as e:
            # qdisc設定がない場合のエラーは無視
            if 'RTNETLINK answers: No such file or directory' in str(e.stderr):
                print(f"  {container} ({interface}): 遅延設定なし(スキップ)")
                return True
            
            print(f"エラー: {container} での tc コマンド実行に失敗: {e}")
            if e.stderr:
                print(f"  詳細: {e.stderr.decode() if isinstance(e.stderr, bytes) else e.stderr}")
            return False
        except Exception as e:
            print(f"エラー: {container} での遅延削除に失敗: {e}")
            return False


def main():
    """メイン処理"""
    cluster_info = ClusterInfo()
    
    # コンテナ発見
    if not cluster_info.discover_containers():
        print("Cluster*_LB パターンのコンテナが見つかりませんでした.")
        sys.exit(1)
    
    # コンテナ情報表示
    cluster_info.print_container_info()
    
    # ネットワーク情報収集
    cluster_info.collect_network_info()
    
    # ネットワーク情報表示
    cluster_info.print_network_info()
    
    # 隣接情報読み込み
    if cluster_info.load_adjacency_info():
        # 隣接情報表示
        cluster_info.print_adjacency_matrix()
    
    # ユーザが遅延を設定する対象を指定
    choice_type, selected_links, delay_config = cluster_info.prompt_delay_configuration()
    
    if choice_type == 4:
        # 遅延なし
        return
    
    if choice_type == 3:
        # 遅延削除
        print("\n全てのクラスタから遅延設定を削除しますか？ (y/n): ", end="")
        confirm = input().strip().lower()
        
        if confirm in ['y', 'yes']:
            cluster_info.remove_delay()
            print("\n=== 削除完了 ===")
        else:
            print("\n削除をキャンセルしました")
        return
    
    # 選択されたリンクの確認
    print(f"\n設定内容:")
    print(f"  対象リンク数: {len(selected_links)}")
    
    # 遅延設定の統計情報を表示
    delays = list(delay_config.values())
    if delays:
        unique_delays = set(delays)
        if len(unique_delays) == 1:
            print(f"  遅延時間: {delays[0]}ms (全リンク同一)")
        else:
            print(f"  遅延時間: {min(delays)}ms 〜 {max(delays)}ms (ランダム)")
            print(f"  平均遅延: {sum(delays) / len(delays):.1f}ms")
    
    # 実際のtc設定を実行
    print("\n遅延設定を適用しますか？ (y/n): ", end="")
    confirm = input().strip().lower()
    
    if confirm in ['y', 'yes']:
        cluster_info.apply_delay(delay_config)
        print("\n=== 設定完了 ===")
    else:
        print("\n設定をキャンセルしました")
        print("\n=== 調査完了 ===")

if __name__ == "__main__":
    main()
