const CY_STYLE = [
    {
        selector: 'node',
        style: {
            label: 'data(label)',
            width: '70px',
            height: '40px',
            shape: 'round-rectangle',
            'background-color': '#e8f4ff',
            'border-width': 2,
            'border-color': '#007acc',
            'text-valign': 'center',
            'text-halign': 'center',
            'font-size': '12px',
            color: '#005999',
        },
    },
    {
        selector: "node[type='HN']",
        style: {
            'background-color': '#fff7e6',
            'border-color': '#fa8c16',
            color: '#ad4e00',
        },
    },
    {
        selector: "node[type='SN']",
        style: {
            'background-color': '#f6ffed',
            'border-color': '#52c41a',
            color: '#237804',
        },
    },
    {
        selector: 'edge',
        style: {
            width: 2,
            'line-color': '#999',
            'target-arrow-color': '#999',
            'target-arrow-shape': 'triangle',
            'curve-style': 'bezier',
            label: 'data(label)',
            'font-size': '11px',
            'text-rotation': 'autorotate',
        },
    },
];

const LINK_MODE_STATUS = 'Select source node, then destination node to create a link.';

export function createTopologyView({
    panelElement,
    containerElement,
    statusElement,
    buttons,
}) {
    if (!panelElement || !containerElement || !buttons) {
        throw new Error('Topology view requires panel, container, and buttons.');
    }

    const state = {
        cy: null,
        graphElement: null,
        version: 0,
        dirty: false,
        history: [],
        redo: [],
        mode: 'idle',
        linkSourceId: null,
        loading: false,
        lastSource: 'frame',
        lastConfigHash: null,
    };

    attachButtonHandlers();

    function activate() {
        if (!state.cy) {
            initializeGraph();
        }
        containerElement.focus({ preventScroll: true });
        loadFromServer();
    }

    function deactivate() {
        setStatus('');
        exitLinkMode();
    }

    function handleFrame(frame) {
        if (!frame || state.dirty) {
            return;
        }
        const configHash = frame.configHash || null;
        if (!configHash || configHash === state.lastConfigHash) {
            return;
        }
        state.lastConfigHash = configHash;
        if (!state.loading) {
            loadFromServer();
        }
    }

    function initializeGraph() {
        containerElement.innerHTML = '';
        state.graphElement = document.createElement('div');
        state.graphElement.style.width = '100%';
        state.graphElement.style.height = '100%';
        containerElement.appendChild(state.graphElement);

        state.cy = cytoscape({
            container: state.graphElement,
            elements: [],
            style: CY_STYLE,
            layout: { name: 'preset' },
            wheelSensitivity: 0.2,
        });

        state.cy.on('tap', 'node', (evt) => {
            if (state.mode === 'add-link') {
                handleLinkSelection(evt.target.id());
            }
        });

        state.cy.on('dbltap', 'node', (evt) => {
            const node = evt.target;
            const newLabel = prompt('Node label', node.data('label') || '');
            if (newLabel !== null && newLabel.trim() !== '') {
                node.data('label', newLabel.trim());
                markDirty();
            }
        });

        state.cy.on('dbltap', 'edge', (evt) => {
            const edge = evt.target;
            const newLabel = prompt('Link label', edge.data('label') || '');
            if (newLabel !== null && newLabel.trim() !== '') {
                edge.data('label', newLabel.trim());
                markDirty();
            }
        });

        state.cy.on('cxttap', (evt) => {
            if (evt.target && evt.target.isNode && evt.target.isNode()) {
                evt.target.select();
            }
        });

        containerElement.addEventListener('keydown', handleKeyPress);
        containerElement.setAttribute('tabindex', '-1');
    }

    function attachButtonHandlers() {
        buttons.addNode?.addEventListener('click', addNode);
        buttons.addLink?.addEventListener('click', toggleLinkMode);
        buttons.delete?.addEventListener('click', deleteSelection);
        buttons.undo?.addEventListener('click', undo);
        buttons.redo?.addEventListener('click', redo);
        buttons.save?.addEventListener('click', saveDraft);
        buttons.reload?.addEventListener('click', loadFromServer);
    }

    function handleKeyPress(event) {
        if (event.key === 'Delete') {
            deleteSelection();
        }
    }

    function addNode() {
        ensureReady();
        const label = prompt('Node label', 'Node');
        if (label === null || label.trim() === '') {
            return;
        }
        const type = prompt("Node type (RN, HN, SN)", 'RN');
        if (type === null || type.trim() === '') {
            return;
        }
        const id = nextNodeId();
        const position = state.cy?.nodes().length
            ? { x: Math.random() * 400 + 100, y: Math.random() * 300 + 100 }
            : { x: 200, y: 200 };

        state.cy.add({
            group: 'nodes',
            data: { id: String(id), label: label.trim(), type: type.trim() },
            position,
        });
        state.cy.center();
        recordSnapshot();
        markDirty();
    }

    function toggleLinkMode() {
        ensureReady();
        if (state.mode === 'add-link') {
            exitLinkMode();
        } else {
            state.mode = 'add-link';
            state.linkSourceId = null;
            setStatus(LINK_MODE_STATUS, 'info');
        }
    }

    function handleLinkSelection(nodeId) {
        if (!state.linkSourceId) {
            state.linkSourceId = nodeId;
            setStatus('Select destination node to complete the link.', 'info');
            return;
        }

        if (state.linkSourceId === nodeId) {
            setStatus('Cannot create self-loop link.', 'warn');
            return;
        }

        const label = prompt('Link label', 'Link');
        if (label === null) {
            exitLinkMode();
            return;
        }
        const latencyInput = prompt('Link latency (cycles)', '1');
        const latency = latencyInput ? parseInt(latencyInput, 10) : 0;

        state.cy.add({
            group: 'edges',
            data: {
                id: `e${state.linkSourceId}-${nodeId}-${Date.now()}`,
                source: state.linkSourceId,
                target: nodeId,
                label: label.trim(),
                latency: Number.isFinite(latency) ? latency : 0,
            },
        });
        recordSnapshot();
        markDirty();
        exitLinkMode();
    }

    function deleteSelection() {
        ensureReady();
        const selection = state.cy.$(':selected');
        if (selection.length === 0) {
            setStatus('Select nodes or links to delete.', 'info');
            return;
        }
        recordSnapshot();
        selection.remove();
        markDirty();
        setStatus('Selected elements deleted.', 'success');
    }

    function undo() {
        if (state.history.length === 0) {
            return;
        }
        const snapshot = state.history.pop();
        state.redo.push(captureSnapshot());
        applySnapshot(snapshot);
        state.dirty = state.history.length > 0;
        setStatus('Undo applied.', 'info');
    }

    function redo() {
        if (state.redo.length === 0) {
            return;
        }
        const snapshot = state.redo.pop();
        state.history.push(captureSnapshot());
        applySnapshot(snapshot);
        state.dirty = true;
        setStatus('Redo applied.', 'info');
    }

    function recordSnapshot() {
        state.history.push(captureSnapshot());
        if (state.history.length > 50) {
            state.history.shift();
        }
        state.redo = [];
    }

    function captureSnapshot() {
        const nodes = state.cy
            ? state.cy.nodes().map((node) => ({
                  id: Number(node.id()),
                  label: node.data('label'),
                  type: node.data('type'),
                  position: node.position(),
              }))
            : [];
        const edges = state.cy
            ? state.cy.edges().map((edge) => ({
                  source: Number(edge.source().id()),
                  target: Number(edge.target().id()),
                  label: edge.data('label'),
                  latency: edge.data('latency') || 0,
              }))
            : [];
        return { nodes, edges };
    }

    function applySnapshot(snapshot) {
        if (!state.cy) {
            return;
        }
        state.cy.elements().remove();
        const nodeElements = snapshot.nodes.map((node) => ({
            group: 'nodes',
            data: {
                id: String(node.id),
                label: node.label,
                type: node.type,
            },
            position: node.position || { x: 100, y: 100 },
        }));
        const edgeElements = snapshot.edges.map((edge, idx) => ({
            group: 'edges',
            data: {
                id: `edge-${idx}-${edge.source}-${edge.target}`,
                source: String(edge.source),
                target: String(edge.target),
                label: edge.label,
                latency: edge.latency,
            },
        }));
        state.cy.add(nodeElements.concat(edgeElements));
        state.cy.center();
    }

    function saveDraft() {
        ensureReady();
        const payload = toDraftPayload();
        state.loading = true;
        setStatus('Saving topology draft...', 'info');
        fetch('/api/topology', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload),
        })
            .then((response) => {
                if (!response.ok) {
                    throw new Error(`HTTP ${response.status}`);
                }
                return response.json();
            })
            .then((data) => {
                state.version = data.version || Date.now();
                state.dirty = false;
                state.lastSource = data.source || 'draft';
                setStatus('Draft saved successfully.', 'success');
            })
            .catch((error) => {
                console.error('Failed to save topology draft:', error);
                setStatus(`Failed to save draft: ${error.message}`, 'error');
            })
            .finally(() => {
                state.loading = false;
            });
    }

    function toDraftPayload() {
        const snapshot = captureSnapshot();
        return {
            nodes: snapshot.nodes.map((node) => ({
                id: node.id,
                label: node.label,
                type: node.type,
                posX: node.position?.x || 0,
                posY: node.position?.y || 0,
            })),
            edges: snapshot.edges,
            version: state.version,
        };
    }

    function loadFromServer() {
        if (state.loading) {
            return;
        }
        state.loading = true;
        setStatus('Loading topology...', 'info');
        fetch('/api/topology')
            .then((response) => {
                if (!response.ok) {
                    throw new Error(`HTTP ${response.status}`);
                }
                return response.json();
            })
            .then((data) => {
                applyServerDraft(data);
                state.dirty = false;
                state.version = data.version || Date.now();
                state.lastSource = data.source || 'frame';
                setStatus('Topology loaded.', 'success');
                state.loading = false;
            })
            .catch((error) => {
                console.error('Failed to load topology:', error);
                setStatus(`Failed to load topology: ${error.message}`, 'error');
                state.loading = false;
            });
    }

    function applyServerDraft(draft) {
        ensureReady();
        state.cy.elements().remove();
        if (!draft || !Array.isArray(draft.nodes)) {
            return;
        }
        const nodes = draft.nodes.map((node, idx) => ({
            group: 'nodes',
            data: {
                id: String(node.id),
                label: node.label,
                type: node.type,
            },
            position: {
                x: typeof node.posX === 'number' && Number.isFinite(node.posX) ? node.posX : 120 * idx + 80,
                y: typeof node.posY === 'number' && Number.isFinite(node.posY) ? node.posY : 200,
            },
        }));
        state.cy.add(nodes);

        if (Array.isArray(draft.edges)) {
            draft.edges.forEach((edge, idx) => {
                state.cy.add({
                    group: 'edges',
                    data: {
                        id: `edge-${idx}-${edge.source}-${edge.target}`,
                        source: String(edge.source),
                        target: String(edge.target),
                        label: edge.label || '',
                        latency: edge.latency || 0,
                    },
                });
            });
        }
        state.cy.layout({ name: 'preset' }).run();
        state.cy.center();
        state.history = [];
        state.redo = [];
    }

    function nextNodeId() {
        const nodes = state.cy ? state.cy.nodes() : [];
        let maxId = 0;
        nodes.forEach((node) => {
            const id = parseInt(node.id(), 10);
            if (Number.isFinite(id) && id > maxId) {
                maxId = id;
            }
        });
        return maxId + 1;
    }

    function markDirty() {
        state.dirty = true;
        setStatus('Topology modified. Remember to save draft.', 'warn');
    }

    function exitLinkMode() {
        state.mode = 'idle';
        state.linkSourceId = null;
        setStatus('');
    }

    function ensureReady() {
        if (!state.cy) {
            initializeGraph();
        }
    }

    function setStatus(message, level = 'info') {
        if (!statusElement) {
            return;
        }
        statusElement.textContent = message || '';
        statusElement.style.color = level === 'error' ? '#c33' : level === 'warn' ? '#d48806' : level === 'success' ? '#389e0d' : '#666';
    }

    return {
        activate,
        deactivate,
        handleFrame,
    };
}
