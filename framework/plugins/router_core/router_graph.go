package router

import "container/list"

type Graph struct {
	adj map[int][]edge
}

type edge struct {
	to      int
	latency int
}

func NewGraph() *Graph {
	return &Graph{adj: make(map[int][]edge)}
}

func (g *Graph) AddEdge(from, to, latency int) {
	if latency <= 0 {
		latency = 1
	}
	g.adj[from] = append(g.adj[from], edge{to: to, latency: latency})
}

func (g *Graph) NextHop(source, target int) (int, int, bool) {
	if source == target {
		return target, 0, true
	}
	type state struct {
		node    int
		latency int
	}
	queue := list.New()
	queue.PushBack(state{node: source, latency: 0})
	visited := map[int]bool{source: true}
	parent := map[int]int{}
	latencies := map[int]int{source: 0}

	for queue.Len() > 0 {
		elem := queue.Front()
		queue.Remove(elem)
		cur := elem.Value.(state)
		if cur.node == target {
			break
		}
		for _, e := range g.adj[cur.node] {
			if visited[e.to] {
				continue
			}
			visited[e.to] = true
			parent[e.to] = cur.node
			latencies[e.to] = cur.latency + e.latency
			queue.PushBack(state{node: e.to, latency: cur.latency + e.latency})
		}
	}

	if !visited[target] {
		return 0, 0, false
	}
	next := target
	for {
		p, ok := parent[next]
		if !ok {
			return 0, 0, false
		}
		if p == source {
			return next, latencies[target], true
		}
		next = p
	}
}

func (g *Graph) ShortestPath(source, target int) ([]int, bool) {
	hop, _, ok := g.NextHop(source, target)
	if !ok {
		return nil, false
	}
	path := []int{source, hop}
	for hop != target {
		nextHop, _, ok := g.NextHop(hop, target)
		if !ok {
			return nil, false
		}
		path = append(path, nextHop)
		hop = nextHop
	}
	return path, true
}
