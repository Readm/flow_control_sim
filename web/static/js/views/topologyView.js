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
        loading: false,
        lastSource: 'frame',
        lastConfigHash: null,
        contextMenu: null,
        contextMenuType: null,
        contextMenuTarget: null,
        edgehandles: null,
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
        hideContextMenu();
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

        state.cy.on('cxttap', 'node', (evt) => {
            evt.originalEvent?.preventDefault();
            showContextMenu('node', evt.target, evt.renderedPosition);
        });

        state.cy.on('cxttap', 'edge', (evt) => {
            evt.originalEvent?.preventDefault();
            showContextMenu('edge', evt.target, evt.renderedPosition);
        });

        state.cy.on('cxttap', () => {
            hideContextMenu();
        });

        state.cy.on('tap', () => {
            hideContextMenu();
        });

        initializeEdgeHandles();
        initializeContextMenu();

        containerElement.addEventListener('keydown', handleKeyPress);
        containerElement.setAttribute('tabindex', '-1');
        state.graphElement.addEventListener('contextmenu', (event) => {
            event.preventDefault();
        });
    }

    function attachButtonHandlers() {
        buttons.addNode?.addEventListener('click', addNode);
        buttons.addLink?.addEventListener('click', showLinkHandleHint);
        buttons.delete?.addEventListener('click', deleteSelection);
        buttons.undo?.addEventListener('click', undo);
        buttons.redo?.addEventListener('click', redo);
        buttons.save?.addEventListener('click', saveDraft);
        buttons.reload?.addEventListener('click', loadFromServer);
    }

    function handleKeyPress(event) {
        if (event.key === 'Escape') {
            hideContextMenu();
            return;
        }
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

    function showLinkHandleHint() {
        ensureReady();
        hideContextMenu();
        setStatus('Drag from the blue handle on a node to another node to create a link.', 'info');
    }

    function deleteSelection() {
        ensureReady();
        hideContextMenu();
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

    function initializeContextMenu() {
        if (state.contextMenu) {
            return;
        }
        const menu = document.createElement('div');
        menu.className = 'topology-context-menu';
        containerElement.appendChild(menu);
        state.contextMenu = menu;
        document.addEventListener('click', handleDocumentClick);
    }

    function handleDocumentClick(event) {
        if (!state.contextMenu || !state.contextMenu.classList.contains('show')) {
            return;
        }
        if (state.contextMenu.contains(event.target)) {
            return;
        }
        hideContextMenu();
    }

    function showContextMenu(type, element, renderedPosition) {
        initializeContextMenu();
        if (!state.contextMenu || !element) {
            return;
        }
        state.contextMenuType = type;
        state.contextMenuTarget = element;
        try {
            element.select();
        } catch (error) {
            // Ignore selection errors for non-selectable elements.
        }
        populateContextMenu(type, element);

        const position = renderedPosition || element.renderedPosition();
        if (!position) {
            return;
        }
        const containerRect = containerElement.getBoundingClientRect();
        state.contextMenu.style.left = '0px';
        state.contextMenu.style.top = '0px';
        state.contextMenu.style.visibility = 'hidden';
        state.contextMenu.classList.add('show');

        const menuRect = state.contextMenu.getBoundingClientRect();
        let left = position.x - containerRect.left;
        let top = position.y - containerRect.top;

        if (left + menuRect.width > containerRect.width) {
            left = containerRect.width - menuRect.width - 8;
        }
        if (top + menuRect.height > containerRect.height) {
            top = containerRect.height - menuRect.height - 8;
        }

        left = Math.max(0, left);
        top = Math.max(0, top);

        state.contextMenu.style.left = `${left}px`;
        state.contextMenu.style.top = `${top}px`;
        state.contextMenu.style.visibility = 'visible';
    }

    function hideContextMenu() {
        if (!state.contextMenu) {
            return;
        }
        state.contextMenu.classList.remove('show');
        state.contextMenu.style.visibility = '';
        state.contextMenu.innerHTML = '';
        state.contextMenuType = null;
        state.contextMenuTarget = null;
    }

    function populateContextMenu(type, element) {
        if (!state.contextMenu) {
            return;
        }
        state.contextMenu.innerHTML = '';
        const items = type === 'node'
            ? [
                  { label: 'Edit Node', handler: () => editNode(element) },
                  { label: 'Delete Node', handler: () => deleteElement(element, 'Node deleted.') },
              ]
            : [
                  { label: 'Edit Link', handler: () => editEdge(element) },
                  { label: 'Delete Link', handler: () => deleteElement(element, 'Link deleted.') },
              ];

        items.forEach((item) => {
            const button = document.createElement('button');
            button.type = 'button';
            button.textContent = item.label;
            button.addEventListener('click', () => {
                hideContextMenu();
                item.handler();
            });
            state.contextMenu.appendChild(button);
        });
    }

    function editNode(node) {
        if (!node || node.removed()) {
            return;
        }
        const currentLabel = node.data('label') || '';
        const currentType = node.data('type') || 'RN';
        const newLabel = prompt('Node label', currentLabel);
        if (newLabel === null || newLabel.trim() === '') {
            return;
        }
        const newType = prompt('Node type (RN, HN, SN)', currentType);
        if (newType === null || newType.trim() === '') {
            return;
        }
        recordSnapshot();
        node.data('label', newLabel.trim());
        node.data('type', newType.trim());
        markDirty();
        setStatus('Node updated.', 'success');
    }

    function editEdge(edge) {
        if (!edge || edge.removed()) {
            return;
        }
        const currentLabel = edge.data('label') || '';
        const currentLatency = edge.data('latency');
        const newLabel = prompt('Link label', currentLabel);
        if (newLabel === null || newLabel.trim() === '') {
            return;
        }
        const latencyPrompt = prompt(
            'Link latency (cycles)',
            Number.isFinite(currentLatency) ? String(currentLatency) : '1',
        );
        if (latencyPrompt === null) {
            return;
        }
        const latencyValue = latencyPrompt.trim();
        const latency = latencyValue === '' ? 0 : parseInt(latencyValue, 10);
        recordSnapshot();
        edge.data('label', newLabel.trim());
        edge.data('latency', Number.isFinite(latency) && latency >= 0 ? latency : 0);
        markDirty();
        setStatus('Link updated.', 'success');
    }

    function deleteElement(element, successMessage) {
        if (!element || element.removed()) {
            return;
        }
        recordSnapshot();
        element.remove();
        markDirty();
        setStatus(successMessage || 'Element deleted.', 'success');
    }

    function initializeEdgeHandles() {
        if (!state.cy) {
            return;
        }
        if (typeof state.cy.edgehandles !== 'function' && typeof cytoscapeEdgehandles === 'function' && typeof cytoscape.use === 'function') {
            cytoscape.use(cytoscapeEdgehandles);
        }
        if (typeof state.cy.edgehandles !== 'function') {
            console.warn('cytoscape-edgehandles plugin is not available.');
            return;
        }
        state.edgehandles = state.cy.edgehandles({
            handleNodes: 'node',
            handlePosition: 'right middle',
            handleColor: '#1890ff',
            handleSize: 10,
            hoverDelay: 100,
            enabled: true,
            loopAllowed: () => false,
        });

        state.cy.on('ehcomplete', (_event, _sourceNode, _targetNode, addedEdge) => {
            if (!addedEdge) {
                return;
            }
            handleEdgeCreation(addedEdge);
        });

        state.cy.on('ehcancel', () => {
            hideContextMenu();
        });
    }

    function handleEdgeCreation(edge) {
        const newLabel = prompt('Link label', 'Link');
        if (newLabel === null || newLabel.trim() === '') {
            edge.remove();
            setStatus('Link creation cancelled.', 'info');
            return;
        }
        const latencyPrompt = prompt(
            'Link latency (cycles)',
            Number.isFinite(edge.data('latency')) ? String(edge.data('latency')) : '1',
        );
        if (latencyPrompt === null) {
            edge.remove();
            setStatus('Link creation cancelled.', 'info');
            return;
        }
        const latencyValue = latencyPrompt.trim();
        const latency = latencyValue === '' ? 0 : parseInt(latencyValue, 10);
        edge.data('label', newLabel.trim());
        edge.data('latency', Number.isFinite(latency) && latency >= 0 ? latency : 0);
        recordSnapshot();
        markDirty();
        setStatus('Link created.', 'success');
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
