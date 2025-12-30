import requests
import json
import time
import os

BASE_URL = "http://localhost:8081"

# Disable proxies
os.environ["HTTP_PROXY"] = ""
os.environ["HTTPS_PROXY"] = ""

def debug_api():
    # Create session that ignores environment proxies
    session = requests.Session()
    session.trust_env = False

    # 1. Load Preset
    print("Loading Preset...")
    resp = session.post(f"{BASE_URL}/load_preset", json={"name": "bi_ring", "params": {"nodes": 16}})
    print(f"Response Status: {resp.status_code}")
    if resp.status_code != 200:
        print(f"Failed to load preset: '{resp.text}'")
        return

    data = resp.json()
    print("Initial State check:")
    check_node_ports(data, "Initial")

    # 2. Advance Simulation
    print("\nAdvancing Simulation to cycle 20...")
    # /advance_to expects query param 'cycle'
    resp = session.post(f"{BASE_URL}/advance_to?cycle=20", json={})
    if resp.status_code != 200:
        print(f"Failed to advance: {resp.text}")
        return

    # Fetch updated state
    resp = session.get(f"{BASE_URL}/load_networks")
    data_list = resp.json()
    if not data_list:
        print("No networks returned")
        return
    
    data = data_list[0]
    print("\nState after Cycle 20:")
    check_node_ports(data, "Cycle 20")

def check_node_ports(data, label):
    if "nodes" not in data:
        print(f"[{label}] No nodes found in response!")
        return
    
    nodes = data["nodes"]
    if not nodes:
        print(f"[{label}] Node list is empty!")
        return

    # Check first node
    node = nodes[0]
    print(f"[{label}] Node 0 ID: {node.get('node_id')}")
    
    in_ports = node.get("in_ports", [])
    out_ports = node.get("out_ports", [])
    
    print(f"[{label}] InPorts count: {len(in_ports)}")
    print(f"[{label}] InPorts Raw: {json.dumps(in_ports, indent=2)}")
    
    print(f"[{label}] OutPorts count: {len(out_ports)}")
    print(f"[{label}] OutPorts Raw: {json.dumps(out_ports, indent=2)}")

    # Check edges for link status
    edges = data.get("edges", [])
    active_links = 0
    for e in edges:
        display = e.get("display", {})
        status = display.get("link_status", [])
        for s in status:
            if s.get("name") == "occupancy":
                vals = s.get("values", [])
                if any(v > 0 for v in vals):
                    active_links += 1
    print(f"[{label}] Active Links (occupancy > 0): {active_links}/{len(edges)}")

if __name__ == "__main__":
    debug_api()
