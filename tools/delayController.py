#!/usr/bin/env python3
"""
delayController.py - Cluster Link Delay Control Tool

Docker containers (Cluster*_LB) network information is collected,
and adjacent cluster information is displayed in matrix form.
"""

import os
import sys
import json
import subprocess
import re
import argparse
from typing import Dict, List, Set, Tuple, Optional


class ClusterInfo:
    """Class to manage cluster information"""
    
    def __init__(self):
        self.containers: List[str] = []
        self.cluster_count: int = 0
        self.interface_map: Dict[str, str] = {}  # {cluster_id: interface_name}
        self.ip_map: Dict[str, str] = {}  # {cluster_id: ip_address}
        self.adjacency: Dict[str, Set[str]] = {}  # {cluster_id: set(adjacent_ids)}
        self.adjacency_ips: Dict[str, Dict[str, str]] = {}  # {cluster_id: {adjacent_id: ip}}
        self.current_delays: Dict[Tuple[str, str], int] = {}  # {(src_id, dst_id): delay_ms}
    
    def discover_containers(self) -> bool:
        """Discover containers matching the Cluster*_LB pattern"""
        try:
            result = subprocess.run(
                ['docker', 'ps', '--format', '{{.Names}}'],
                capture_output=True,
                text=True,
                check=True
            )
            
            # Filter by Cluster*_LB pattern
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
            print(f"Error: Failed to execute docker ps: {e}", file=sys.stderr)
            return False
    
    def extract_cluster_id(self, container_name: str) -> Optional[str]:
        """Extract cluster ID from container name (Cluster0_LB -> 0)"""
        match = re.match(r'Cluster(\d+)_LB', container_name)
        return match.group(1) if match else None
    
    def get_interface_info(self, container: str) -> Optional[Tuple[str, str]]:
        """Get interface name and IP address starting with 172.x.x.x from container"""
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
                # Interface name line
                if line and not line.startswith(' '):
                    parts = line.split()
                    if parts:
                        current_interface = parts[0].rstrip(':')
                
                # Search for inet line starting with 172.
                if 'inet ' in line and '172.' in line:
                    # Old format: inet addr:172.18.4.2 or New format: inet 172.18.4.2
                    match = re.search(r'inet (?:addr:)?(\d+\.\d+\.\d+\.\d+)', line)
                    if match:
                        ip = match.group(1)
                        if ip.startswith('172.'):
                            return (current_interface, ip.split('/')[0])
            
            return None
            
        except subprocess.CalledProcessError:
            return None
    
    def collect_network_info(self):
        """Collect network information of each container"""
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
        """Load adjacency information from adjacentList.json"""
        if not os.path.exists(json_path):
            print(f"Error: {json_path} not found")
            return False
        
        try:
            with open(json_path, 'r') as f:
                data = json.load(f)
            
            # Build adjacency information
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
            print(f"Error: {e}", file=sys.stderr)
            return False
    
    def _print_adjacency_matrix(self, cluster_ids: List[str]):
        """Print adjacency information in matrix form (with delay information)"""
        # Header row
        header = "    " + " ".join([f"{cid:>4}" for cid in cluster_ids])
        print(header)
        print("    " + "-" * (5 * len(cluster_ids)))
        
        # Each row
        for src_id in cluster_ids:
            row = f"{src_id:>3} |"
            for dst_id in cluster_ids:
                if src_id == dst_id:
                    row += "   - "
                elif dst_id in self.adjacency.get(src_id, set()):
                    # Display one-way delay (from smaller ID to larger ID)
                    if int(src_id) < int(dst_id):
                        link_key = (src_id, dst_id)
                    else:
                        link_key = (dst_id, src_id)
                    
                    delay = self.current_delays.get(link_key, 0)
                    if delay > 0:
                        row += f"{delay:>4} "
                    else:
                        row += "   O "
                else:
                    row += "   . "
            print(row)
    
    def print_container_info(self):
        """Print container information"""
        print("=== Checking Cluster LB containers ===")
        if not self.containers:
            print("No containers matching the Cluster*_LB pattern were found.")
            return
        
        print("Discovered Cluster LB containers:")
        for container in self.containers:
            print(f"  {container}")
        print(f"Number of discovered clusters: {self.cluster_count}")
    
    def print_network_info(self):
        """Print network information"""
        print("\n=== Inter-cluster network information ===")
        for cluster_id in sorted(self.ip_map.keys(), key=int):
            ip = self.ip_map[cluster_id]
            interface = self.interface_map.get(cluster_id, "Unknown")
            print(f"{cluster_id}: {ip} ({interface})")
        
        print("\n=== Collecting cluster information ===")
        for cluster_id in sorted(self.interface_map.keys(), key=int):
            print(f"{cluster_id}: {self.interface_map[cluster_id]}")
    
    def print_adjacency_matrix(self):
        """Print adjacency information in matrix form"""
        cluster_ids = sorted(self.adjacency.keys(), key=int)
        self._print_adjacency_matrix(cluster_ids)
    
    def get_all_links(self) -> List[Tuple[str, str]]:
        """Get a list of all links (no duplicates)"""
        links = []
        processed = set()
        
        for src_id in sorted(self.adjacency.keys(), key=int):
            for dst_id in self.adjacency[src_id]:
                # Avoid duplicates for bidirectional links (smaller ID first)
                link_pair = tuple(sorted([src_id, dst_id], key=int))
                if link_pair not in processed:
                    processed.add(link_pair)
                    links.append(link_pair)
        
        return links
    
    def prompt_delay_configuration(self) -> Tuple[int, List[Tuple[str, str]], Dict[Tuple[str, str], int]]:
        """
        Ask the user for delay configuration
        
        Returns:
            (selection type, selected link list, delay configuration dictionary {(src, dst): delay_ms})
        """
        print("\n=== Link Delay Configuration ===")
        print("Select the target for delay configuration:")
        print("  0. Set delay for all links")
        print("  1. Manually set delay for some links")
        print("  2. Remove existing delay settings")
        print("  3. No delay (exit)")
        
        while True:
            try:
                choice = input("\nSelect (0-3): ").strip()
                if choice not in ['0', '1', '2', '3']:
                    print("Error: Please enter one of 0, 1, 2, 3")
                    continue
                
                choice_num = int(choice)
                
                # 3. No delay
                if choice_num == 3:
                    print("Skipping delay configuration.")
                    return (4, [], {})
                
                # 2. Remove existing delay settings
                if choice_num == 2:
                    return (3, [], {})
                
                # 0. All links
                if choice_num == 0:
                    all_links = self.get_all_links()
                    delay_config = self._configure_all_links_delay(all_links)
                    if delay_config:
                        return (1, all_links, delay_config)
                    else:
                        continue
                
                # 1. Some links (manual selection)
                if choice_num == 1:
                    selected_links = self._select_specific_links()
                    if selected_links:
                        delay_ms = self._get_delay_input()
                        delay_config = {link: delay_ms for link in selected_links}
                        print(f"\nSetting {delay_ms}ms delay for {len(selected_links)} selected links.")
                        return (2, selected_links, delay_config)
                    else:
                        print("No links were selected.")
                        continue
                        
            except KeyboardInterrupt:
                print("\n\nInterrupted.")
                sys.exit(0)
            except Exception as e:
                print(f"Error: {e}")
                continue
    
    def _configure_all_links_delay(self, all_links: List[Tuple[str, str]]) -> Optional[Dict[Tuple[str, str], int]]:
        """
        Select delay configuration method for all links
        
        Returns:
            Delay configuration dictionary {(src, dst): delay_ms} or None
        """
        print(f"\nSelect delay configuration method for all {len(all_links)} links:")
        print("  0. Manually set the same delay")
        print("  1. Assign random delays")
        
        while True:
            try:
                method = input("\nSelect (0-1): ").strip()
                if method not in ['0', '1']:
                    print("Error: Please enter 0 or 1")
                    continue
                
                if method == '0':
                    # Manually set the same delay
                    delay_ms = self._get_delay_input()
                    delay_config = {link: delay_ms for link in all_links}
                    print(f"\nSetting {delay_ms}ms delay for all {len(all_links)} links.")
                    return delay_config
                
                else:
                    # Assign random delays
                    delay_config = self._assign_random_delays(all_links)
                    if delay_config:
                        return delay_config
                    else:
                        continue
                        
            except Exception as e:
                print(f"Error: {e}")
                continue
    
    def _assign_random_delays(self, links: List[Tuple[str, str]]) -> Optional[Dict[Tuple[str, str], int]]:
        """
        Assign random delays to links
        
        Returns:
            Delay configuration dictionary {(src, dst): delay_ms} or None
        """
        import random
        
        print("\nRandom delay configuration:")
        
        # Enter delay range
        while True:
            try:
                min_delay_input = input("Minimum delay (ms) [default: 10]: ").strip()
                min_delay = int(min_delay_input) if min_delay_input else 10
                
                max_delay_input = input("Maximum delay (ms) [default: 100]: ").strip()
                max_delay = int(max_delay_input) if max_delay_input else 100
                
                if min_delay < 0 or max_delay < 0:
                    print("Error: Please enter positive integers")
                    continue
                
                if min_delay > max_delay:
                    print("Error: Minimum delay must be less than or equal to maximum delay")
                    continue
                
                break
                
            except ValueError:
                print("Error: Please enter a number")
        
        # Assign random delays
        delay_config = {}
        for link in links:
            delay_config[link] = random.randint(min_delay, max_delay)
        
        # Sample display
        print(f"\nRandomly assigned delays (sample, first 5 links):")
        for i, (link, delay) in enumerate(list(delay_config.items())[:5], 1):
            src, dst = link
            print(f"  {i}. Cluster {src} - Cluster {dst}: {delay}ms")
        
        if len(links) > 5:
            print(f"  ... and {len(links) - 5} more links")
        
        confirm = input("\nIs this configuration okay? (y/n): ").strip().lower()
        if confirm in ['y', 'yes']:
            return delay_config
        else:
            return None
    
    def _get_delay_input(self) -> int:
        """Get delay time input"""
        while True:
            try:
                delay_input = input("Delay time (ms) [default: 10]: ").strip()
                if not delay_input:
                    return 10
                delay_ms = int(delay_input)
                if delay_ms < 0:
                    print("Error: Please enter a positive integer")
                    continue
                return delay_ms
            except ValueError:
                print("Error: Please enter a number")
    
    def _select_specific_links(self) -> List[Tuple[str, str]]:
        """Select specific links"""
        all_links = self.get_all_links()
        
        print("\nAvailable links:")
        for i, (src, dst) in enumerate(all_links, 1):
            print(f"  {i}. Cluster {src} - Cluster {dst}")
        
        print("\nSelection methods:")
        print("  - Single: 1")
        print("  - Multiple: 1,3,5")
        print("  - Range: 1-5")
        print("  - Mixed: 1,3-5,7")
        
        while True:
            try:
                selection = input("\nEnter link numbers: ").strip()
                if not selection:
                    print("Error: Input is empty")
                    continue
                
                selected_indices = self._parse_selection(selection, len(all_links))
                selected_links = [all_links[i-1] for i in selected_indices]
                
                # Confirmation display
                print("\nSelected links:")
                for src, dst in selected_links:
                    print(f"  - Cluster {src} - Cluster {dst}")
                
                confirm = input("\nIs this okay? (y/n): ").strip().lower()
                if confirm in ['y', 'yes']:
                    return selected_links
                
            except ValueError as e:
                print(f"Error: {e}")
    

    def _parse_selection(self, selection: str, max_num: int) -> List[int]:
        """Parse selection string (e.g., "1,3-5,7" -> [1,3,4,5,7])"""
        indices = set()
        
        for part in selection.split(','):
            part = part.strip()
            
            # Range specification (e.g., 3-5)
            if '-' in part:
                try:
                    start, end = part.split('-')
                    start, end = int(start.strip()), int(end.strip())
                    if start < 1 or end > max_num or start > end:
                        raise ValueError(f"Range {start}-{end} is invalid (must be within 1-{max_num})")
                    indices.update(range(start, end + 1))
                except ValueError as e:
                    raise ValueError(f"Invalid range specification: {part}")
            
            # Single specification (e.g., 3)
            else:
                try:
                    num = int(part)
                    if num < 1 or num > max_num:
                        raise ValueError(f"Number {num} is invalid (must be within 1-{max_num})")
                    indices.add(num)
                except ValueError:
                    raise ValueError(f"Invalid number: {part}")
        
        return sorted(indices)
    
    def apply_delay(self, delay_config: Dict[Tuple[str, str], int]) -> bool:
        """
        Use the tc command to set delay on links (one direction only)
        
        Args:
            delay_config: Dictionary of {(src_id, dst_id): delay_ms}
        
        Returns:
            True if successful
        """
        print("\n=== Applying delay settings ===")
        print("Note: Delay is set in one direction only")
        
        success_count = 0
        fail_count = 0
        
        for (src_id, dst_id), delay_ms in delay_config.items():
            # Set delay in one direction only (from smaller ID to larger ID)
            if int(src_id) < int(dst_id):
                direction_src, direction_dst = src_id, dst_id
            else:
                direction_src, direction_dst = dst_id, src_id
            
            if self._apply_delay_single_direction(direction_src, direction_dst, delay_ms):
                success_count += 1
                # Update current_delays on success
                self.current_delays[(direction_src, direction_dst)] = delay_ms
            else:
                fail_count += 1
        
        print(f"\nSettings applied: Success {success_count}, Fail {fail_count}")
        return fail_count == 0
    
    def _apply_delay_single_direction(self, src_id: str, dst_id: str, delay_ms: int) -> bool:
        """
        Set delay in a single direction
        
        Args:
            src_id: Source cluster ID
            dst_id: Destination cluster ID
            delay_ms: Delay time (milliseconds)
        
        Returns:
            True if successful
        """
        # Get container name
        container = f"Cluster{src_id}_LB"
        
        # Get interface name
        interface = self.interface_map.get(src_id)
        if not interface:
            print(f"Error: Interface for cluster {src_id} not found")
            return False
        
        # Get destination IP address
        dst_ip = self.adjacency_ips.get(src_id, {}).get(dst_id)
        if not dst_ip:
            print(f"Error: Destination IP for cluster {src_id} -> {dst_id} not found")
            return False
        
        try:
            # Check if tc command is available
            check_result = subprocess.run(
                ['docker', 'exec', '--privileged', container, 'sh', '-c', 'command -v tc'],
                capture_output=True,
                text=True
            )
            
            if check_result.returncode != 0:
                print(f"Warning: tc command is not available in {container}")
                return False
            
            # 1. Check qdisc settings and initialize if necessary
            check_qdisc = subprocess.run(
                ['docker', 'exec', '--privileged', container, 'tc', 'qdisc', 'show', 'dev', interface],
                capture_output=True,
                text=True
            )
            
            # Add prio qdisc if not present
            if 'prio' not in check_qdisc.stdout:
                subprocess.run(
                    ['docker', 'exec', '--privileged', container, 'tc', 'qdisc', 'add', 'dev', interface,
                     'root', 'handle', '1:', 'prio', 'bands', '3'],
                    check=True,
                    capture_output=True
                )
                print(f"  {container} ({interface}): prio qdisc set")
            
            # 2. Set netem qdisc
            subprocess.run(
                ['docker', 'exec', '--privileged', container, 'tc', 'qdisc', 'replace', 'dev', interface,
                 'parent', '1:3', 'handle', '30:', 'netem', 'delay', f'{delay_ms}ms'],
                check=True,
                capture_output=True
            )
            
            # 3. Distribute traffic based on destination IP using u32 filter
            subprocess.run(
                ['docker', 'exec', '--privileged', container, 'tc', 'filter', 'add', 'dev', interface,
                 'protocol', 'ip', 'parent', '1:0', 'prio', '1', 'u32',
                 'match', 'ip', 'dst', f'{dst_ip}/32', 'flowid', '1:3'],
                check=True,
                capture_output=True
            )
            
            print(f"  {container} -> {dst_ip} ({dst_id}): {delay_ms}ms delay set")
            return True
            
        except subprocess.CalledProcessError as e:
            print(f"Error: Failed to execute tc command in {container}: {e}")
            if e.stderr:
                print(f"  Details: {e.stderr.decode() if isinstance(e.stderr, bytes) else e.stderr}")
            return False
        except Exception as e:
            print(f"Error: Failed to set delay in {container}: {e}")
            return False
    
    def remove_delay(self) -> bool:
        """
        Remove delay settings from all containers
        
        Returns:
            True if successful
        """
        print("\n=== Removing delay settings ===")
        
        success_count = 0
        fail_count = 0
        
        for cluster_id in sorted(self.interface_map.keys(), key=int):
            if self._remove_delay_single_container(cluster_id):
                success_count += 1
            else:
                fail_count += 1
        
        # Clear current_delays if all removals were successful
        if fail_count == 0:
            self.current_delays.clear()
        
        print(f"\nRemoval complete: Success {success_count}, Fail {fail_count}")
        return fail_count == 0
    
    def _remove_delay_single_container(self, cluster_id: str) -> bool:
        """
        Remove delay settings from a single container
        
        Args:
            cluster_id: Cluster ID
        
        Returns:
            True if successful
        """
        container = f"Cluster{cluster_id}_LB"
        interface = self.interface_map.get(cluster_id)
        
        if not interface:
            print(f"Error: Interface for cluster {cluster_id} not found")
            return False
        
        try:
            # Check if tc command is available
            check_result = subprocess.run(
                ['docker', 'exec', '--privileged', container, 'sh', '-c', 'command -v tc'],
                capture_output=True,
                text=True
            )
            
            if check_result.returncode != 0:
                print(f"Warning: tc command is not available in {container}")
                return False
            
            # Check qdisc settings
            check_qdisc = subprocess.run(
                ['docker', 'exec', '--privileged', container, 'tc', 'qdisc', 'show', 'dev', interface],
                capture_output=True,
                text=True
            )
            
            # Skip if no qdisc settings
            if 'qdisc prio' not in check_qdisc.stdout and 'qdisc noqueue' in check_qdisc.stdout:
                print(f"  {container} ({interface}): No delay settings (skip)")
                return True
            
            # Delete root qdisc (removes all filters and nested qdiscs)
            subprocess.run(
                ['docker', 'exec', '--privileged', container, 'tc', 'qdisc', 'del', 'dev', interface, 'root'],
                check=True,
                capture_output=True
            )
            
            print(f"  {container} ({interface}): Removed delay settings")
            return True
            
        except subprocess.CalledProcessError as e:
            # Ignore errors if no qdisc settings
            if 'RTNETLINK answers: No such file or directory' in str(e.stderr):
                print(f"  {container} ({interface}): No delay settings (skip)")
                return True
            
            print(f"Error: Failed to execute tc command in {container}: {e}")
            if e.stderr:
                print(f"  Details: {e.stderr.decode() if isinstance(e.stderr, bytes) else e.stderr}")
            return False
        except Exception as e:
            print(f"Error: Failed to remove delay in {container}: {e}")
            return False


def parse_arguments():
    """Parse command-line arguments"""
    parser = argparse.ArgumentParser(
        description='Cluster Link Delay Control Tool',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog='''
Usage:
  python3 delayController.py [mode] [options...]

Modes:
  0 : Set delay on all links
  1 : Set delay on selected links
  2 : Remove delay
  3 : No delay (information only)

Example:
  # Interactive mode (no arguments)
  python3 delayController.py
  
  # mode=0: Set fixed delay on all links
  python3 delayController.py 0 10          # Set 10ms delay on all links
  python3 delayController.py 0 50          # Set 50ms delay on all links
  
  # mode=0: Set random delay on all links
  python3 delayController.py 0 10:100      # Set random delay of 10-100ms on all links
  python3 delayController.py 0 5:50        # Set random delay of 5-50ms on all links
  
  # mode=1: Set delay on selected links (single setting)
  python3 delayController.py 1 1 20        # Set 20ms delay on link 1
  python3 delayController.py 1 1,3,5 30    # Set 30ms delay on links 1, 3, and 5
  python3 delayController.py 1 1-5 15      # Set 15ms delay on links 1 through 5
  python3 delayController.py 1 1,3-5,7 25  # Set 25ms delay on links 1, 3 through 5, and 7
  
  # mode=1: Set delay on specific links and others (multiple settings)
  python3 delayController.py 1 3=10 other=1:5       # Set 10ms delay on link 3, 1-5ms random delay on others
  python3 delayController.py 1 1,3=20 other=5:50    # Set 20ms delay on links 1, 3, 5-50ms random delay on others
  python3 delayController.py 1 1-3=100 other=10     # Set 100ms delay on links 1-3, 10ms delay on others
  
  # mode=2: Remove delay
  python3 delayController.py 2
  
  # mode=3: No delay
  python3 delayController.py 3
        ''')
    
    parser.add_argument('mode', nargs='?', type=int, choices=[0, 1, 2, 3],
                        help='Operation mode: 0=all links, 1=selected links, 2=remove, 3=none')
    
    parser.add_argument('args', nargs='*', type=str,
                        help='mode=0: delay value or range (10 or 10:100), mode=1: link=delay pairs (1=10 2-3=20 4=5:50)')
    
    return parser.parse_args()


def process_command_line_args(args, cluster_info: ClusterInfo) -> Tuple[int, List[Tuple[str, str]], Dict[Tuple[str, str], int]]:
    """
    Process command-line arguments and return settings
    
    Args:
        args: argparse arguments
        cluster_info: ClusterInfo instance
    
    Returns:
        (selection type, selected link list, delay configuration dictionary)
    """
    import random
    
    mode = args.mode
    
    # mode=3: No delay
    if mode == 3:
        return (4, [], {})
    
    # mode=2: Remove delay
    if mode == 2:
        return (3, [], {})
    
    all_links = cluster_info.get_all_links()
    
    # mode=0: Set delay on all links
    if mode == 0:
        if not args.args or len(args.args) == 0:
            print("Error: Please specify a delay value or range (e.g., 10 or 10:100)")
            sys.exit(1)
        
        selected_links = all_links
        choice_type = 1
        
        delay_spec = args.args[0]
        
        # Parse delay value (fixed or random)
        if ':' in delay_spec:
            # Random delay (e.g., 10:100)
            try:
                min_delay, max_delay = delay_spec.split(':')
                min_delay = int(min_delay)
                max_delay = int(max_delay)
                
                if min_delay > max_delay:
                    print("Error: Minimum delay must be less than or equal to maximum delay")
                    sys.exit(1)
                
                delay_config = {}
                for link in selected_links:
                    delay_config[link] = random.randint(min_delay, max_delay)
            except ValueError:
                print(f"Error: Invalid range specification: {delay_spec}")
                sys.exit(1)
        else:
            # Fixed delay (e.g., 10)
            try:
                delay_ms = int(delay_spec)
                delay_config = {link: delay_ms for link in selected_links}
            except ValueError:
                print(f"Error: Invalid delay value: {delay_spec}")
                sys.exit(1)
    
    # mode=1: Set delay on selected links
    elif mode == 1:
        if not args.args or len(args.args) == 0:
            print("Error: Please specify links")
            print("Example 1: python3 delayController.py 1 1,3-5 20           # Traditional format")
            print("Example 2: python3 delayController.py 1 3=10 other=1:5     # Multiple format")
            sys.exit(1)
        
        delay_config = {}
        selected_links = []
        
        # Determine if multiple format (contains '=')
        if '=' in args.args[0]:
            # Multiple format: specific_link=delay other=delay
            choice_type = 2
            
            if len(args.args) != 2:
                print("Error: Multiple format requires exactly two arguments")
                print("Example: python3 delayController.py 1 3=10 other=1:5")
                sys.exit(1)
            
            # 1st: specific link settings
            first_spec = args.args[0]
            # 2nd: other link settings
            second_spec = args.args[1]
            
            if not second_spec.startswith('other='):
                print("Error: The second argument must be in the format 'other=delay'")
                print("Example: python3 delayController.py 1 3=10 other=1:5")
                sys.exit(1)
            
            try:
                # Parse specific links
                links_part, delay_part = first_spec.split('=', 1)
                
                # Parse link numbers
                specific_indices = cluster_info._parse_selection(links_part, len(all_links))
                specific_links = [all_links[i-1] for i in specific_indices]
                
                # Parse delay value (fixed or random)
                if ':' in delay_part:
                    # Random delay
                    min_delay, max_delay = delay_part.split(':')
                    min_delay = int(min_delay)
                    max_delay = int(max_delay)
                    
                    if min_delay > max_delay:
                        print("Error: Minimum delay must be less than or equal to maximum delay")
                        sys.exit(1)
                    
                    for link in specific_links:
                        delay_config[link] = random.randint(min_delay, max_delay)
                else:
                    # Fixed delay
                    delay_ms = int(delay_part)
                    for link in specific_links:
                        delay_config[link] = delay_ms
                
                # Parse other links
                other_delay_part = second_spec.split('=', 1)[1]
                
                # Other links (all links excluding specific links)
                other_links = [link for link in all_links if link not in specific_links]
                
                # Parse other delay values
                if ':' in other_delay_part:
                    # Random delay
                    min_delay, max_delay = other_delay_part.split(':')
                    min_delay = int(min_delay)
                    max_delay = int(max_delay)
                    
                    if min_delay > max_delay:
                        print("Error: Minimum delay must be less than or equal to maximum delay")
                        sys.exit(1)
                    
                    for link in other_links:
                        delay_config[link] = random.randint(min_delay, max_delay)
                else:
                    # Fixed delay
                    delay_ms = int(other_delay_part)
                    for link in other_links:
                        delay_config[link] = delay_ms
                
                # Add all links to the selected list
                selected_links = all_links
                
            except ValueError as e:
                print(f"Error: Failed to parse arguments: {e}")
                sys.exit(1)
        
        else:
            # Traditional format: link numbers and delay value
            if len(args.args) < 2:
                print("Error: Please specify link numbers and delay value")
                print("Example: python3 delayController.py 1 1,3-5 20")
                sys.exit(1)
            
            choice_type = 2
            
            # Parse link numbers
            try:
                selected_indices = cluster_info._parse_selection(args.args[0], len(all_links))
                selected_links = [all_links[i-1] for i in selected_indices]
            except ValueError as e:
                print(f"Error: {e}")
                sys.exit(1)
            
            # Parse delay value
            try:
                delay_ms = int(args.args[1])
                delay_config = {link: delay_ms for link in selected_links}
            except ValueError:
                print(f"Error: Invalid delay value: {args.args[1]}")
                sys.exit(1)
    
    else:
        print(f"Error: Unknown mode: {mode}")
        sys.exit(1)
    
    return (choice_type, selected_links, delay_config)


def main():
    """Main processing"""
    args = parse_arguments()
    cluster_info = ClusterInfo()
    
    # Discover containers
    if not cluster_info.discover_containers():
        print("No containers with the Cluster*_LB pattern were found.")
        sys.exit(1)
    
    # Display container information
    cluster_info.print_container_info()
    
    # Collect network information
    cluster_info.collect_network_info()
    
    # Display network information
    cluster_info.print_network_info()
    
    # Load adjacency information
    if cluster_info.load_adjacency_info():
        # Display adjacency information
        print("\n=== Adjacency Cluster Information (Matrix Format: Connection Status) ===")
        cluster_info.print_adjacency_matrix()
    
    # Command-line argument mode vs Interactive mode
    if args.mode is not None:
        # Command-line argument mode
        choice_type, selected_links, delay_config = process_command_line_args(
            args, cluster_info)
    else:
        # Interactive mode
        choice_type, selected_links, delay_config = cluster_info.prompt_delay_configuration()
    
    if choice_type == 4:
        # No delay
        return
    
    if choice_type == 3:
        # Remove delay
        cluster_info.remove_delay()
        print("\n=== Removal Complete ===")
        return
    
    # Confirm selected links
    print(f"\nConfiguration:")
    print(f"  Number of target links: {len(selected_links)}")
    
    # Display statistics of delay settings
    delays = list(delay_config.values())
    if delays:
        unique_delays = set(delays)
        if len(unique_delays) == 1:
            print(f"  Delay time: {delays[0]}ms (Same for all links)")
        else:
            print(f"  Delay time: {min(delays)}ms 〜 {max(delays)}ms (Random)")
            print(f"  Average delay: {sum(delays) / len(delays):.1f}ms")
    
    # Apply actual tc settings
    cluster_info.apply_delay(delay_config)
    
    # Display adjacency matrix after delay settings
    print("\n=== Adjacency Cluster Information (Matrix Format: After Settings) ===")
    cluster_info.print_adjacency_matrix()
    
    print("\n=== Configuration Complete ===")

if __name__ == "__main__":
    main()
