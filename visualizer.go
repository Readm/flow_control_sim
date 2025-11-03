package main

import (
	"strconv"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// FyneVisualizer implements Visualizer interface using Fyne GUI
type FyneVisualizer struct {
	app            fyne.App
	window         fyne.Window
	nodes          map[int]*Node
	nodeWidgets    map[int]*widget.Card
	container      *container.Grid
	mu             sync.RWMutex
	headless       bool
	currentCycle   *widget.Label
	pauseButton    *widget.Button
	resumeButton   *widget.Button
	resetButton    *widget.Button
	isPaused       bool
	pauseChan      chan bool
	resumeChan     chan bool
}

// NewFyneVisualizer creates a new Fyne-based visualizer
func NewFyneVisualizer() *FyneVisualizer {
	v := &FyneVisualizer{
		nodes:       make(map[int]*Node),
		nodeWidgets: make(map[int]*widget.Card),
		headless:    false,
		isPaused:    false,
		pauseChan:   make(chan bool, 1),
		resumeChan:  make(chan bool, 1),
	}
	return v
}

// Initialize initializes the GUI window (only called in non-headless mode)
func (v *FyneVisualizer) Initialize() {
	if v.headless {
		return
	}
	v.app = app.New()
	v.window = v.app.NewWindow("Flow Control Simulator")
	v.window.Resize(fyne.NewSize(1200, 800))

	// Create cycle display
	v.currentCycle = widget.NewLabel("Cycle: 0")

	// Create control buttons
	v.pauseButton = widget.NewButton("Pause", func() {
		v.isPaused = true
		select {
		case v.pauseChan <- true:
		default:
		}
	})
	v.resumeButton = widget.NewButton("Resume", func() {
		v.isPaused = false
		select {
		case v.resumeChan <- true:
		default:
		}
	})
	v.resetButton = widget.NewButton("Reset", func() {
		// Reset will be handled by simulator
	})

	// Control panel
	controlPanel := container.NewHBox(
		v.currentCycle,
		widget.NewSeparator(),
		v.pauseButton,
		v.resumeButton,
		v.resetButton,
	)

	// Create grid container for nodes
	v.container = container.NewGridWithColumns(0) // will be set dynamically

	// Main content
	content := container.NewBorder(
		controlPanel,
		nil,
		nil,
		nil,
		v.container,
	)

	v.window.SetContent(content)
}

// UpdateNode updates the visualization for a specific node
func (v *FyneVisualizer) UpdateNode(node *Node) {
	if v.headless || node == nil {
		return
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	v.nodes[node.ID] = node

	// Update or create widget for this node
	card, exists := v.nodeWidgets[node.ID]
	if !exists {
		card = v.createNodeWidget(node)
		v.nodeWidgets[node.ID] = card
	} else {
		v.updateNodeWidget(card, node)
	}

	// Refresh the container layout
	if v.container != nil {
		v.container.Refresh()
	}
}

// createNodeWidget creates a widget card for displaying a node
func (v *FyneVisualizer) createNodeWidget(node *Node) *widget.Card {
	content := v.createNodeWidgetContent(node)

	card := widget.NewCard(
		"",
		"",
		content,
	)

	return card
}

// updateNodeWidget updates an existing node widget with new data
// For simplicity, we recreate the entire card content
func (v *FyneVisualizer) updateNodeWidget(card *widget.Card, node *Node) {
	// Recreate the card content with updated data
	newContent := v.createNodeWidgetContent(node)
	card.SetContent(newContent)
}

// createNodeWidgetContent creates the content for a node widget
func (v *FyneVisualizer) createNodeWidgetContent(node *Node) fyne.CanvasObject {
	idLabel := widget.NewLabel("ID: " + strconv.Itoa(node.ID))
	idLabel.Alignment = fyne.TextAlignCenter

	var nodeType string
	switch node.Type {
	case NodeTypeMaster:
		nodeType = "Master"
	case NodeTypeSlave:
		nodeType = "Slave"
	case NodeTypeRelay:
		nodeType = "Relay"
	default:
		nodeType = "Unknown"
	}

	typeLabel := widget.NewLabel(nodeType)
	typeLabel.Importance = widget.MediumImportance

	// Queue information
	var queueLabels []fyne.CanvasObject
	for _, q := range node.Queues {
		queueText := q.Name + ": " + strconv.Itoa(q.Length)
		queueLabel := widget.NewLabel(queueText)
		queueLabels = append(queueLabels, queueLabel)
	}

	content := container.NewVBox(
		idLabel,
		typeLabel,
		widget.NewSeparator(),
	)
	if len(queueLabels) > 0 {
		queueContainer := container.NewVBox(queueLabels...)
		content.Add(queueContainer)
	} else {
		content.Add(widget.NewLabel("No queues"))
	}

	return content
}

// Render updates the display (called after all nodes are updated)
func (v *FyneVisualizer) Render() {
	if v.headless || v.container == nil {
		return
	}

	v.mu.RLock()
	defer v.mu.RUnlock()

	// Update grid layout with all node widgets
	var widgets []fyne.CanvasObject

	// Group nodes by type for better layout
	masters := make([]*widget.Card, 0)
	slaves := make([]*widget.Card, 0)
	relays := make([]*widget.Card, 0)

	for id, card := range v.nodeWidgets {
		node := v.nodes[id]
		if node == nil {
			continue
		}
		switch node.Type {
		case NodeTypeMaster:
			masters = append(masters, card)
		case NodeTypeSlave:
			slaves = append(slaves, card)
		case NodeTypeRelay:
			relays = append(relays, card)
		}
	}

	// Add masters first
	for _, card := range masters {
		widgets = append(widgets, card)
	}
	// Then relays
	for _, card := range relays {
		widgets = append(widgets, card)
	}
	// Then slaves
	for _, card := range slaves {
		widgets = append(widgets, card)
	}

	// Update grid columns based on content
	cols := len(masters)
	if len(relays) > cols {
		cols = len(relays)
	}
	if len(slaves) > cols {
		cols = len(slaves)
	}
	if cols == 0 {
		cols = 3 // default
	}

	// Recreate container with new layout
	v.container.Objects = widgets
	v.container.Refresh()
}

// UpdateCycle updates the current cycle display
func (v *FyneVisualizer) UpdateCycle(cycle int) {
	if v.headless || v.currentCycle == nil {
		return
	}
	v.currentCycle.SetText("Cycle: " + strconv.Itoa(cycle))
}

// SetHeadless sets headless mode
func (v *FyneVisualizer) SetHeadless(headless bool) {
	v.headless = headless
}

// IsHeadless returns whether visualizer is in headless mode
func (v *FyneVisualizer) IsHeadless() bool {
	return v.headless
}

// ShowAndRun shows the window and runs the application (blocks)
func (v *FyneVisualizer) ShowAndRun() {
	if v.headless {
		return
	}
	v.window.ShowAndRun()
}

// Close closes the window
func (v *FyneVisualizer) Close() {
	if v.headless || v.window == nil {
		return
	}
	v.window.Close()
}

// WaitForPause waits for pause signal (non-blocking check)
func (v *FyneVisualizer) WaitForPause() bool {
	if v.headless {
		return false
	}
	select {
	case <-v.pauseChan:
		return true
	default:
		return false
	}
}

// WaitForResume waits for resume signal (non-blocking check)
func (v *FyneVisualizer) WaitForResume() bool {
	if v.headless {
		return false
	}
	select {
	case <-v.resumeChan:
		return true
	default:
		return false
	}
}

// IsPaused returns whether simulation is paused
func (v *FyneVisualizer) IsPaused() bool {
	return v.isPaused
}


