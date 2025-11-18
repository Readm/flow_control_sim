const NODE_WIDTH = 60;
const BASE_NODE_HEIGHT = 60;
const PROGRESS_BAR_HEIGHT = 3;
const PROGRESS_BAR_SPACING = 1;
const PROGRESS_PADDING = 4;

export function createFlowView({
    panelElement,
    cyElement,
    overlayElement,
    loadingElement,
    showPacketModal,
}) {
    if (!panelElement || !cyElement || !overlayElement) {
        throw new Error("Flow view requires panel, Cytoscape, and overlay elements.");
    }

    let cy = null;
    let topologyInitialized = false;
    let lastConfigHash = null;
    let expectingReset = false;
    let currentFrame = null;

    const resizeHandler = debounce(() => {
        if (!cy || !topologyInitialized) {
            return;
        }
        applyCustomLayout();
        if (currentFrame) {
            drawPipelineStatePoints(currentFrame);
        }
    }, 200);

    window.addEventListener("resize", resizeHandler);

    function ensureInitialized() {
        if (cy) {
            return;
        }

        cy = cytoscape({
            container: cyElement,
            elements: [],
            style: buildCyStyle(),
            layout: { name: "preset" },
            wheelSensitivity: 0.2,
        });

        if (typeof cyqtip === "function") {
            cyqtip(cytoscape);
        }

        cy.on("mouseover", "node", (evt) => {
            const tooltip = buildNodeTooltip(evt.target.data());
            attachOrUpdateTooltip(evt.target, tooltip);
        });

        cy.on("mouseover", "edge", (evt) => {
            const tooltip = buildEdgeTooltip(evt.target.data());
            attachOrUpdateTooltip(evt.target, tooltip);
        });

        cy.on("tap", "node", (evt) => {
            if (typeof showPacketModal !== "function") {
                return;
            }
            const modalData = buildPacketModalContent(evt.target.data());
            showPacketModal(modalData);
        });

        const redraw = () => {
            if (currentFrame) {
                drawPipelineStatePoints(currentFrame);
            }
        };

        cy.on("render", redraw);
        cy.on("pan", () => {
            hideAllTooltips();
            redraw();
        });
        cy.on("zoom", () => {
            hideAllTooltips();
            redraw();
        });
        cy.on("panzoom", () => {
            hideAllTooltips();
            redraw();
        });
    }

    function activate() {
        ensureInitialized();
        if (!cy) {
            return;
        }
        cy.resize();
        requestAnimationFrame(() => {
            applyCustomLayout();
            if (currentFrame) {
                drawPipelineStatePoints(currentFrame);
            }
        });
    }

    function deactivate() {
        hideAllTooltips();
    }

    function destroy() {
        window.removeEventListener("resize", resizeHandler);
        if (cy) {
            cy.destroy();
            cy = null;
        }
        overlayElement.innerHTML = "";
        topologyInitialized = false;
        lastConfigHash = null;
        currentFrame = null;
        expectingReset = false;
    }

    function updateFrame(frame) {
        if (!frame) {
            return;
        }

        ensureInitialized();
        currentFrame = frame;
        setLoadingVisible(false);
        hideAllTooltips();

        const configHash = frame.configHash || null;
        let shouldReinitialize = false;

        if (expectingReset && frame.cycle === 0) {
            shouldReinitialize = true;
            expectingReset = false;
        } else if (configHash && lastConfigHash && configHash !== lastConfigHash) {
            shouldReinitialize = true;
        } else if (!lastConfigHash && configHash) {
            lastConfigHash = configHash;
        }

        if (shouldReinitialize && cy) {
            cy.elements().remove();
            topologyInitialized = false;
            if (configHash) {
                lastConfigHash = configHash;
            }
        }

        if (!topologyInitialized) {
            initializeTopology(frame);
            topologyInitialized = true;
            if (configHash) {
                lastConfigHash = configHash;
            }
        }

        updateNodes(frame.nodes || []);
        updateEdges(frame.edges || [], frame.cycle || 0);
        drawPipelineStatePoints(frame);
    }

    function setExpectingReset(flag) {
        expectingReset = Boolean(flag);
    }

    function initializeTopology(frame) {
        if (!cy) {
            return;
        }

        const elements = [];

        (frame.nodes || []).forEach((node) => {
            const { height } = calculateNodeDimensions(node.queues || []);
            const progressBg = createProgressBarDataUri(node.queues || [], height);
            elements.push({
                group: "nodes",
                data: {
                    id: String(node.id),
                    label: buildNodeLabel(node),
                    type: node.type,
                    queues: node.queues || [],
                    payload: node.payload || {},
                    capabilities: node.capabilities || [],
                    height,
                    progressBg,
                },
            });
        });

        (frame.edges || []).forEach((edge) => {
            elements.push({
                group: "edges",
                data: {
                    id: `e${edge.source}-${edge.target}`,
                    source: String(edge.source),
                    target: String(edge.target),
                    label: `${edge.label} (${edge.latency}cy)`,
                    latency: edge.latency,
                    pipelineStages: edge.pipelineStages || [],
                    bandwidthLimit: edge.bandwidthLimit,
                },
            });
        });

        cy.add(elements);
        requestAnimationFrame(() => applyCustomLayout());
    }

    function updateNodes(nodes) {
        if (!cy) {
            return;
        }
        nodes.forEach((node) => {
            const cyNode = cy.getElementById(String(node.id));
            if (!cyNode || cyNode.length === 0) {
                return;
            }
            const { height } = calculateNodeDimensions(node.queues || []);
            const progressBg = createProgressBarDataUri(node.queues || [], height);
            cyNode.data({
                label: buildNodeLabel(node),
                queues: node.queues || [],
                payload: node.payload || {},
                capabilities: node.capabilities || [],
                height,
                progressBg,
            });
        });
    }

    function updateEdges(edges, frameCycle) {
        if (!cy) {
            return;
        }
        edges.forEach((edge) => {
            const cyEdge = cy.getElementById(`e${edge.source}-${edge.target}`);
            if (!cyEdge || cyEdge.length === 0) {
                return;
            }
            const pipelineStages = edge.pipelineStages || [];
            cyEdge.data({
                label: `${edge.label} (${edge.latency}cy)`,
                pipelineStages,
                bandwidthLimit: edge.bandwidthLimit,
                latency: edge.latency,
            });
            const hasTraffic = pipelineStages.some((stage) => stage.packetCount > 0);
            if (hasTraffic) {
                cyEdge.data("lastActiveCycle", frameCycle);
            }
            const lastActiveCycle = cyEdge.data("lastActiveCycle");
            const recentlyActive = typeof lastActiveCycle === "number" && frameCycle - lastActiveCycle <= 5;
            if (hasTraffic || recentlyActive) {
                cyEdge.style("line-color", "#ff4d4f");
                cyEdge.style("width", 3);
            } else {
                cyEdge.style("line-color", "#91d5ff");
                cyEdge.style("width", 2);
            }
        });
    }

    function applyCustomLayout() {
        if (!cy) {
            return;
        }
        const width = cy.width();
        const height = cy.height();
        if (width === 0 || height === 0) {
            return;
        }
        if (isRingTopology()) {
            applyRingLayout(width, height);
            return;
        }

        const leftX = width * 0.2;
        const centerX = width * 0.5;
        const rightX = width * 0.8;

        distributeNodesVertically(cy.nodes('[type="RN"], [type="master"]'), leftX, height);
        cy.nodes('[type="HN"], [type="relay"]').forEach((node) => {
            node.position({ x: centerX, y: height / 2 });
        });
        distributeNodesVertically(cy.nodes('[type="SN"], [type="slave"]'), rightX, height);
    }

    function isRingTopology() {
        if (!cy) {
            return false;
        }
        return cy.nodes('[type="RT"]').length > 0;
    }

    function applyRingLayout(width, height) {
        if (!cy) {
            return;
        }
        const routers = cy.nodes('[type="RT"]');
        if (routers.length === 0) {
            return;
        }
        const centerX = width / 2;
        const centerY = height / 2;
        const radius = Math.min(width, height) * 0.35;
        const innerRadius = radius * 0.6;

        routers.forEach((router, index) => {
            const angle = (2 * Math.PI * index) / routers.length;
            const routerX = centerX + radius * Math.cos(angle);
            const routerY = centerY + radius * Math.sin(angle);
            router.position({ x: routerX, y: routerY });

            const neighbors = router
                .connectedEdges()
                .connectedNodes()
                .filter((node) => node.id() !== router.id() && node.data('type') !== 'RT');

            if (neighbors.length === 0) {
                return;
            }
            const spread = Math.PI / 9;
            const startAngle = angle - spread / 2;
            const endAngle = angle + spread / 2;

            neighbors.forEach((node, idx) => {
                const t = neighbors.length === 1 ? 0.5 : idx / (neighbors.length - 1);
                const nodeAngle = startAngle + (endAngle - startAngle) * t;
                const nodeX = centerX + innerRadius * Math.cos(nodeAngle);
                const nodeY = centerY + innerRadius * Math.sin(nodeAngle);
                node.position({ x: nodeX, y: nodeY });
            });
        });

        const remaining = cy.nodes().filter((node) => {
            if (node.data('type') === 'RT') {
                return false;
            }
            let visited = false;
            routers.forEach((router) => {
                if (visited) {
                    return;
                }
                const neighbors = router.connectedEdges().connectedNodes();
                if (neighbors.anySame(node)) {
                    visited = true;
                }
            });
            return !visited;
        });
        if (remaining.length > 0) {
            distributeNodesVertically(remaining, centerX, height);
        }
    }

    function distributeNodesVertically(collection, x, containerHeight) {
        if (!collection || collection.length === 0) {
            return;
        }
        const spacing = Math.min(containerHeight / Math.max(collection.length + 1, 2), 150);
        const totalHeight = (collection.length - 1) * spacing;
        const startY = (containerHeight - totalHeight) / 2;
        collection.forEach((node, index) => {
            node.position({
                x,
                y: startY + index * spacing,
            });
        });
    }

    function drawPipelineStatePoints(frame) {
        if (!cy) {
            return;
        }
        overlayElement.innerHTML = "";
        const rect = cyElement.getBoundingClientRect();
        overlayElement.setAttribute("width", String(rect.width));
        overlayElement.setAttribute("height", String(rect.height));

        (frame.edges || []).forEach((edge) => {
            const cyEdge = cy.getElementById(`e${edge.source}-${edge.target}`);
            if (!cyEdge || cyEdge.length === 0) {
                return;
            }
            const data = cyEdge.data();
            const latency = data.latency || edge.latency || 1;
            const bandwidthLimit = data.bandwidthLimit || edge.bandwidthLimit || 1;
            const pipelineStages = data.pipelineStages || edge.pipelineStages || [];
            drawPointsOnEdge(cyEdge[0], latency, bandwidthLimit, pipelineStages);
        });
    }

    function drawPointsOnEdge(cyEdge, latency, bandwidthLimit, pipelineStages) {
        if (!cyEdge || latency <= 0 || bandwidthLimit <= 0) {
            return;
        }

        const stageMap = new Map();
        pipelineStages.forEach((stage) => {
            if (stage && typeof stage.stageIndex === "number") {
                stageMap.set(stage.stageIndex, stage.packetCount || 0);
            }
        });

        const source = cyEdge.source();
        const target = cyEdge.target();
        if (!source || !target) {
            return;
        }

        const sourcePos = computeRenderedPosition(source);
        const targetPos = computeRenderedPosition(target);
        const midpoint = computeMidpoint(cyEdge, sourcePos, targetPos);

        for (let stageIdx = 0; stageIdx < latency; stageIdx += 1) {
            const ratio = latency === 1 ? 0.5 : 0.6 - (stageIdx / (latency - 1)) * 0.2;
            const { x: pointX, y: pointY } = interpolatePoint(sourcePos, midpoint, targetPos, ratio);
            const direction = ratio <= 0.5 ? subtract(midpoint, sourcePos) : subtract(targetPos, midpoint);
            const perp = perpendicular(direction);
            const packetCount = stageMap.get(stageIdx) || 0;
            const spacing = 4;
            const totalWidth = (bandwidthLimit - 1) * spacing;
            const startOffset = -totalWidth / 2;

            for (let i = 0; i < bandwidthLimit; i += 1) {
                const offset = startOffset + i * spacing;
                const x = pointX + perp.x * offset;
                const y = pointY + perp.y * offset;
                const filled = i < packetCount;
                const circle = document.createElementNS("http://www.w3.org/2000/svg", "circle");
                circle.setAttribute("cx", String(x));
                circle.setAttribute("cy", String(y));
                circle.setAttribute("r", "3");
                circle.setAttribute("fill", filled ? "#ff4d4f" : "#999");
                circle.setAttribute("stroke", "#fff");
                circle.setAttribute("stroke-width", "0.5");
                overlayElement.appendChild(circle);
            }
        }
    }

    function computeRenderedPosition(node) {
        try {
            const pos = node.renderedPosition();
            if (Number.isFinite(pos.x) && Number.isFinite(pos.y)) {
                return pos;
            }
        } catch (error) {
            // fall through to fallback logic
        }
        const model = node.position();
        const zoom = cy ? cy.zoom() : 1;
        const pan = cy ? cy.pan() : { x: 0, y: 0 };
        return {
            x: model.x * zoom + pan.x,
            y: model.y * zoom + pan.y,
        };
    }

    function computeMidpoint(cyEdge, sourcePos, targetPos) {
        try {
            const midpointModel = cyEdge.midpoint();
            if (midpointModel && Number.isFinite(midpointModel.x) && Number.isFinite(midpointModel.y) && cy) {
                const zoom = cy.zoom();
                const pan = cy.pan();
                return {
                    x: midpointModel.x * zoom + pan.x,
                    y: midpointModel.y * zoom + pan.y,
                };
            }
        } catch (error) {
            // fall back to linear interpolation
        }
        return {
            x: (sourcePos.x + targetPos.x) / 2,
            y: (sourcePos.y + targetPos.y) / 2,
        };
    }

    function interpolatePoint(start, mid, end, ratio) {
        if (ratio <= 0.5) {
            const t = ratio * 2;
            return {
                x: start.x + (mid.x - start.x) * t,
                y: start.y + (mid.y - start.y) * t,
            };
        }
        const t = (ratio - 0.5) * 2;
        return {
            x: mid.x + (end.x - mid.x) * t,
            y: mid.y + (end.y - mid.y) * t,
        };
    }

    function subtract(a, b) {
        return { x: a.x - b.x, y: a.y - b.y };
    }

    function perpendicular(vector) {
        const length = Math.sqrt(vector.x * vector.x + vector.y * vector.y) || 1;
        return { x: vector.y / length, y: -vector.x / length };
    }

    function calculateNodeDimensions(queues) {
        if (!queues || queues.length === 0) {
            return { height: BASE_NODE_HEIGHT };
        }
        const progressHeight = queues.length * PROGRESS_BAR_HEIGHT + (queues.length - 1) * PROGRESS_BAR_SPACING + PROGRESS_PADDING;
        return { height: BASE_NODE_HEIGHT + progressHeight };
    }

    function createProgressBarDataUri(queues, nodeHeight) {
        if (!queues || queues.length === 0) {
            return "none";
        }
        const parts = [`<rect x=\"0\" y=\"0\" width=\"${NODE_WIDTH}\" height=\"${nodeHeight}\" fill=\"transparent\"/>`];
        queues.forEach((queue, index) => {
            const load = calculateQueueLoad(queue);
            const barY = nodeHeight - PROGRESS_PADDING - (index + 1) * PROGRESS_BAR_HEIGHT - index * PROGRESS_BAR_SPACING;
            const width = (load / 100) * NODE_WIDTH;
            const color = getLoadColor(load);
            parts.push(`<rect x=\"0\" y=\"${barY}\" width=\"${NODE_WIDTH}\" height=\"${PROGRESS_BAR_HEIGHT}\" fill=\"#e0e0e0\" opacity=\"0.6\" rx=\"1\"/>`);
            if (width > 0) {
                parts.push(`<rect x=\"0\" y=\"${barY}\" width=\"${width}\" height=\"${PROGRESS_BAR_HEIGHT}\" fill=\"${color}\" rx=\"1\"/>`);
            }
        });
        const svg = `<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"${NODE_WIDTH}\" height=\"${nodeHeight}\">${parts.join("")}</svg>`;
        return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
    }

    function buildNodeLabel(node) {
        const base = node.label || `Node ${node.id}`;
        const queues = node.queues || [];
        if (queues.length === 0) {
            return base;
        }
        const suffix = queues
            .map((queue) => {
                const capacity = queue.capacity === -1 ? "∞" : queue.capacity;
                return `${queue.name}:${queue.length}/${capacity}`;
            })
            .join(" ");
        return `${base}\n${suffix}`;
    }

    function buildNodeTooltip(data) {
        const parts = [
            '<div style="font-size: 12px; line-height: 1.5;">',
            `<div style=\"font-weight: bold; margin-bottom: 4px;\">${data.label || data.type}</div>`,
            `<div style=\"color: #666;\">Type: <strong>${data.type}</strong></div>`,
            `<div style=\"color: #666;\">ID: <strong>${data.id}</strong></div>`,
        ];

        const payloadEntries = Object.entries(data.payload || {}).filter(([key]) => key !== "chiProtocol" && key !== "nodeType");
        if (payloadEntries.length > 0) {
            parts.push('<div style="margin-top: 8px; font-weight: 600;">Metadata</div>');
            payloadEntries.forEach(([key, value]) => {
                parts.push(`<div style=\"font-size: 11px; color: #666;\">${key}: <strong>${value}</strong></div>`);
            });
        }
        const capabilities = Array.isArray(data.capabilities) ? data.capabilities : [];
        if (capabilities.length > 0) {
            parts.push('<div style="margin-top: 8px; font-weight: 600;">Capabilities</div>');
            capabilities.forEach((capability) => {
                parts.push(`<div style=\"font-size: 11px; color: #666;\">${capability}</div>`);
            });
        }
        const queues = data.queues || [];
        if (queues.length > 0) {
            parts.push('<div style="margin-top: 8px; font-weight: 600;">Queues</div>');
            queues.forEach((queue) => {
                const capacity = queue.capacity === -1 ? "∞" : queue.capacity;
                parts.push(`<div style=\"font-size: 11px; color: #666;\">${queue.name}: <strong>${queue.length}/${capacity}</strong></div>`);
            });
        }

        parts.push("</div>");
        return parts.join("");
    }

    function buildEdgeTooltip(data) {
        const parts = [
            '<div style="font-size: 12px; line-height: 1.5;">',
            `<div style=\"font-weight: bold; margin-bottom: 4px;\">${data.label}</div>`,
            `<div style=\"color: #666;\">Latency: <strong>${data.latency || 0}</strong> cycles</div>`,
            `<div style=\"color: #666;\">Bandwidth: <strong>${data.bandwidthLimit || 0}</strong> packets/cycle</div>`,
        ];

        const stages = data.pipelineStages || [];
        if (stages.length > 0) {
            parts.push('<div style="margin-top: 8px; font-weight: 600;">Pipeline</div>');
            stages.forEach((stage) => {
                parts.push(`<div style=\"font-size: 11px; color: #666;\">Stage ${stage.stageIndex}: <strong>${stage.packetCount}</strong> packets</div>`);
            });
        }

        parts.push("</div>");
        return parts.join("");
    }

    function buildPacketModalContent(nodeData) {
        const title = `${nodeData.label || "Node"} - Packet Information`;
        const queues = nodeData.queues || [];
        if (queues.length === 0) {
            return {
                title,
                html: '<div class="queue-section empty">No queues found for this node.</div>',
            };
        }

        const sections = queues.map((queue) => {
            const capacity = queue.capacity === -1 ? "∞" : queue.capacity;
            let html = `<div class=\"queue-section\"><h3>${queue.name} (${queue.length}/${capacity})</h3>`;
            if (!queue.packets || queue.packets.length === 0) {
                html += '<div class="queue-section empty">This queue is empty.</div>';
            } else {
                queue.packets.forEach((packet, index) => {
                    html += `<div class=\"packet-item\"><div class=\"packet-item-header\">Packet #${index + 1} (ID: <span class=\"packet-id\">${packet.id}</span>)</div>`;
                    html += `<div class=\"packet-item-body\">${formatPacketInfo(packet)}</div></div>`;
                });
            }
            html += "</div>";
            return html;
        });

        return {
            title,
            html: sections.join(""),
        };
    }

    function formatPacketInfo(packet) {
        const fields = [
            { label: "Packet ID", key: "id" },
            { label: "Type", key: "type" },
            { label: "Source Node ID", key: "srcID" },
            { label: "Destination Node ID", key: "dstID" },
            { label: "Master ID", key: "masterID" },
            { label: "Request ID", key: "requestID" },
            { label: "Transaction Type", key: "transactionType" },
            { label: "Message Type", key: "messageType" },
            { label: "Response Type", key: "responseType" },
            {
                label: "Address",
                key: "address",
                formatter: (value) => {
                    if (!Number.isFinite(value)) {
                        return value;
                    }
                    return `0x${value.toString(16).toUpperCase()}`;
                },
            },
            { label: "Data Size (bytes)", key: "dataSize" },
            { label: "Generated At (cycle)", key: "generatedAt" },
            { label: "Sent At (cycle)", key: "sentAt" },
            { label: "Received At (cycle)", key: "receivedAt" },
            { label: "Completed At (cycle)", key: "completedAt" },
        ];

        let html = '<table class="packet-table">';
        fields.forEach((field) => {
            const value = packet[field.key];
            if (value === undefined || value === null || value === "") {
                return;
            }
            const formatted = field.formatter ? field.formatter(value) : value;
            html += `<tr><td class=\"packet-field-label\">${field.label}:</td><td class=\"packet-field-value\">${formatted}</td></tr>`;
        });
        html += "</table>";
        return html;
    }

    function calculateQueueLoad(queue) {
        if (!queue || queue.capacity === -1 || queue.capacity === 0) {
            return 0;
        }
        return Math.min(100, Math.max(0, (queue.length / queue.capacity) * 100));
    }

    function getLoadColor(value) {
        if (value < 50) {
            return "#52c41a";
        }
        if (value < 80) {
            return "#faad14";
        }
        return "#ff4d4f";
    }

    function attachOrUpdateTooltip(element, html) {
        if (!element || typeof element.qtip !== "function") {
            return;
        }
        let api = element.qtip("api");
        if (!api) {
            element.qtip({
                content: { text: html },
                position: {
                    my: "top center",
                    at: "bottom center",
                    viewport: cyElement,
                    adjust: { method: "flip shift" },
                },
                style: {
                    classes: "qtip-bootstrap",
                    tip: { width: 16, height: 8 },
                },
                show: { event: "mouseover", solo: true },
                hide: { event: "mouseout", fixed: true, delay: 200 },
            });
            api = element.qtip("api");
        }
        if (api && typeof api.set === "function") {
            api.set("content.text", html);
        }
    }

    function hideAllTooltips() {
        if (window.$ && typeof window.$.fn?.qtip === "function") {
            window.$(".qtip").qtip("hide");
        }
    }

    function setLoadingVisible(visible) {
        if (loadingElement) {
            loadingElement.style.display = visible ? "flex" : "none";
        }
    }

    function buildCyStyle() {
        return [
            {
                selector: "node",
                style: {
                    label: "data(label)",
                    width: `${NODE_WIDTH}px`,
                    height: "data(height)",
                    shape: "round-rectangle",
                    "background-color": "#e8e8e8",
                    "background-image": "data(progressBg)",
                    "background-width": "100%",
                    "background-height": "100%",
                    "background-fit": "cover",
                    "background-position-x": "0%",
                    "background-position-y": "100%",
                    "border-width": 2,
                    "border-color": "#888",
                    "text-valign": "center",
                    "text-halign": "center",
                    "font-size": "12px",
                    "font-weight": "bold",
                    color: "#333",
                    "text-wrap": "wrap",
                    "text-max-width": "80px",
                },
            },
            {
                selector: 'node[type="RN"], node[type="master"]',
                style: {
                    "background-color": "#4a9eff",
                    "border-color": "#0066cc",
                    color: "white",
                },
            },
            {
                selector: 'node[type="SN"], node[type="slave"]',
                style: {
                    "background-color": "#52c41a",
                    "border-color": "#389e0d",
                    color: "white",
                },
            },
            {
                selector: 'node[type="HN"], node[type="relay"]',
                style: {
                    "background-color": "#ff7a45",
                    "border-color": "#d4380d",
                    color: "white",
                },
            },
            {
                selector: "edge",
                style: {
                    width: 2,
                    "line-color": "#999",
                    "target-arrow-color": "#999",
                    "target-arrow-shape": "triangle",
                    "curve-style": "bezier",
                    label: "data(label)",
                    "font-size": "10px",
                    "text-rotation": "autorotate",
                    "text-margin-y": -10,
                },
            },
            {
                selector: 'edge[label*="Req"], edge[label*="request"]',
                style: {
                    "line-color": "#4a9eff",
                    "target-arrow-color": "#4a9eff",
                },
            },
            {
                selector: 'edge[label*="Comp"], edge[label*="response"]',
                style: {
                    "line-color": "#52c41a",
                    "target-arrow-color": "#52c41a",
                },
            },
        ];
    }

    return {
        activate,
        deactivate,
        updateFrame,
        setExpectingReset,
        destroy,
    };
}

function debounce(fn, wait) {
    let timeoutId = null;
    return (...args) => {
        if (timeoutId) {
            clearTimeout(timeoutId);
        }
        timeoutId = setTimeout(() => fn(...args), wait);
    };
}


