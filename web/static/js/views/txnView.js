const DEFAULT_PLACEHOLDER = 'Enter a transaction ID and click Load Timeline.';

export function createTxnView({
    panelElement,
    containerElement,
    inputElement,
    loadButton,
    errorElement,
}) {
    if (!panelElement || !containerElement || !inputElement || !loadButton || !errorElement) {
        throw new Error('Transaction view requires panel, container, input, button, and error elements.');
    }

    initializePlaceholder();
    bindEventHandlers();

    function activate() {
        // No-op for now; reserved for future enhancements.
    }

    function deactivate() {
        // No-op for now; reserved for future enhancements.
    }

    function clear() {
        initializePlaceholder();
        clearError();
    }

    async function handleManualLoad() {
        const txnId = Number.parseInt(inputElement.value, 10);
        await loadTimeline(txnId);
    }

    async function loadTimeline(txnId) {
        if (!Number.isFinite(txnId) || txnId <= 0) {
            showError('Please enter a valid transaction ID');
            return;
        }

        clearError();
        showLoading('Loading transaction timeline...');

        try {
            const response = await fetch(`/api/transaction/${txnId}/timeline`);
            if (!response.ok) {
                if (response.status === 404) {
                    throw new Error(`Transaction ${txnId} not found`);
                }
                throw new Error(`HTTP ${response.status}`);
            }

            const timeline = await response.json();
            if (!timeline || !Array.isArray(timeline.events) || timeline.events.length === 0) {
                containerElement.innerHTML = '<div class="sequence-diagram-error">No events found for this transaction</div>';
                return;
            }

            await renderMermaidSequenceDiagram(containerElement, timeline);
        } catch (error) {
            console.error('Failed to load timeline:', error);
            containerElement.innerHTML = `<div class="sequence-diagram-error">Failed to load transaction timeline: ${error.message}</div>`;
        }
    }

    function initializePlaceholder() {
        containerElement.innerHTML = `<div class="sequence-diagram-loading">${DEFAULT_PLACEHOLDER}</div>`;
    }

    function bindEventHandlers() {
        loadButton.addEventListener('click', () => {
            handleManualLoad();
        });

        inputElement.addEventListener('keypress', (event) => {
            if (event.key === 'Enter') {
                handleManualLoad();
            }
        });
    }

    function showLoading(text) {
        containerElement.innerHTML = `<div class="sequence-diagram-loading">${text}</div>`;
    }

    function showError(message) {
        errorElement.textContent = message;
        errorElement.classList.add('show');
    }

    function clearError() {
        errorElement.textContent = '';
        errorElement.classList.remove('show');
    }

    function buildPacketEdgeKey(packetID, edgeKey) {
        if (!edgeKey) {
            return null;
        }
        return `${packetID}_${edgeKey.fromID}_${edgeKey.toID}`;
    }

    function inferMessageLabel(event, allEvents, sortedNodes) {
        if (!event) {
            return 'Message';
        }
        const packet = event.packet || {};
        if (packet.type) {
            return packet.type;
        }
        if (!event.nodeID) {
            return 'Message';
        }
        const node = sortedNodes.find((n) => n.id === event.nodeID);
        if (!node) {
            return 'Message';
        }
        if (node.type === 'RN') {
            return 'Req';
        }
        if (node.type === 'SN') {
            return 'Resp';
        }
        return 'Message';
    }

    function cycleToPercent(cycle) {
        if (!Number.isFinite(cycle)) {
            return 0;
        }
        return (cycle % 1000) / 10;
    }

    function buildDirective(startPercent, endPercent) {
        const parts = [];
        if (Number.isFinite(startPercent)) {
            parts.push(`ys=${startPercent.toFixed(2)}%`);
        }
        if (Number.isFinite(endPercent)) {
            parts.push(`ye=${endPercent.toFixed(2)}%`);
        }
        return parts.length > 0 ? `[${parts.join(', ')}]` : '';
    }

    function buildMessageDetails(sendCycle, receiveCycle) {
        if (!Number.isFinite(sendCycle) || !Number.isFinite(receiveCycle)) {
            return '';
        }
        const delay = Math.max(0, receiveCycle - sendCycle);
        return ` (cycles ${sendCycle}→${receiveCycle}, delay ${delay})`;
    }

    function convertTimelineToMermaid(timeline) {
        const events = timeline.events || [];
        const nodes = timeline.nodes || [];
        if (events.length === 0) {
            return 'sequenceDiagram\nNote over Client: No events available';
        }

        const sortedNodes = [...nodes].sort((a, b) => a.index - b.index);
        const participants = sortedNodes.map((node, idx) => {
            const alias = `N${idx}`;
            const label = node.label || `Node ${node.id}`;
            return `    participant ${alias} as ${label}`;
        });

        const nodeAliasMap = new Map();
        sortedNodes.forEach((node, idx) => {
            nodeAliasMap.set(node.id, `N${idx}`);
        });

        const sortedEvents = [...events].sort((a, b) => (a.cycle || 0) - (b.cycle || 0));
        const messages = [];
        const activations = new Map();
        const sendEventMap = new Map();

        sortedEvents.forEach((event) => {
            const cycle = event.cycle;
            const nodeAlias = nodeAliasMap.get(event.nodeID);

            switch (event.eventType) {
                case 'PacketSent': {
                    if (!event.edgeKey) {
                        break;
                    }
                    const fromAlias = nodeAliasMap.get(event.edgeKey.fromID);
                    const toAlias = nodeAliasMap.get(event.edgeKey.toID);
                    if (!fromAlias || !toAlias) {
                        break;
                    }
                    const key = buildPacketEdgeKey(event.packetID, event.edgeKey);
                    if (!key) {
                        break;
                    }
                    const messageLabel = inferMessageLabel(event, events, sortedNodes);
                    sendEventMap.set(key, {
                        fromAlias,
                        toAlias,
                        sendCycle: cycle,
                        label: messageLabel,
                    });
                    break;
                }
                case 'PacketInTransitEnd': {
                    if (!event.edgeKey) {
                        break;
                    }
                    const key = buildPacketEdgeKey(event.packetID, event.edgeKey);
                    const fromAlias = nodeAliasMap.get(event.edgeKey.fromID);
                    const toAlias = nodeAliasMap.get(event.edgeKey.toID);
                    if (!fromAlias || !toAlias) {
                        break;
                    }
                    const sendInfo = key ? sendEventMap.get(key) : null;
                    const sendCycle = sendInfo ? sendInfo.sendCycle : cycle;
                    const receiveCycle = cycle;
                    const messageLabel = sendInfo ? sendInfo.label : inferMessageLabel(event, events, sortedNodes);
                    const directive = buildDirective(cycleToPercent(sendCycle), cycleToPercent(receiveCycle));
                    const details = buildMessageDetails(sendCycle, receiveCycle);
                    messages.push(`    ${fromAlias}->>${toAlias}: ${directive} ${messageLabel}${details}`);
                    if (key) {
                        sendEventMap.delete(key);
                    }
                    break;
                }
                case 'PacketEnqueued': {
                    if (!nodeAlias) {
                        break;
                    }
                    const queueKey = `queue_${event.packetID}_${event.nodeID}`;
                    if (!activations.has(queueKey)) {
                        messages.push(`    activate ${nodeAlias}`);
                        activations.set(queueKey, nodeAlias);
                    }
                    break;
                }
                case 'PacketDequeued': {
                    if (!nodeAlias) {
                        break;
                    }
                    const dequeueKey = `queue_${event.packetID}_${event.nodeID}`;
                    if (activations.has(dequeueKey)) {
                        messages.push(`    deactivate ${nodeAlias}`);
                        activations.delete(dequeueKey);
                    }
                    break;
                }
                case 'PacketProcessingStart': {
                    if (!nodeAlias) {
                        break;
                    }
                    const processKey = `process_${event.packetID}_${event.nodeID}`;
                    if (!activations.has(processKey)) {
                        messages.push(`    activate ${nodeAlias}`);
                        activations.set(processKey, nodeAlias);
                    }
                    messages.push(`    Note over ${nodeAlias}: Processing (cycle ${cycle})`);
                    break;
                }
                case 'PacketProcessingEnd': {
                    if (!nodeAlias) {
                        break;
                    }
                    const processKey = `process_${event.packetID}_${event.nodeID}`;
                    if (activations.has(processKey)) {
                        messages.push(`    deactivate ${nodeAlias}`);
                        activations.delete(processKey);
                    }
                    break;
                }
                case 'PacketGenerated': {
                    if (!nodeAlias) {
                        break;
                    }
                    const messageLabel = inferMessageLabel(event, events, sortedNodes);
                    messages.push(`    ${nodeAlias}->>${nodeAlias}: ${messageLabel} (cycle ${cycle})`);
                    break;
                }
                default:
                    break;
            }
        });

        sendEventMap.forEach((info) => {
            const directive = buildDirective(cycleToPercent(info.sendCycle), cycleToPercent(info.sendCycle));
            const details = Number.isFinite(info.sendCycle) ? ` (cycle ${info.sendCycle}, pending)` : '';
            messages.push(`    ${info.fromAlias}->>${info.toAlias}: ${directive} ${info.label}${details}`);
        });

        activations.forEach((alias) => {
            messages.push(`    deactivate ${alias}`);
        });

        return `sequenceDiagram\n${participants.join('\n')}\n\n${messages.join('\n')}`;
    }

    function parseDirectiveStr(input) {
        const result = {};
        if (!input) {
            return result;
        }
        const parts = input.split(',').map((part) => part.trim()).filter(Boolean);
        parts.forEach((part) => {
            let match = part.match(/^([a-zA-Z]+)\s*=\s*([+-]?\d+(\.\d+)?)%$/);
            if (match) {
                const key = match[1];
                result[key] = Number(match[2]);
                result[`${key}Unit`] = 'percent';
                return;
            }
            match = part.match(/^([a-zA-Z]+)\s*=\s*([+-]?\d+(\.\d+)?)$/);
            if (match) {
                const key = match[1];
                result[key] = Number(match[2]);
                result[`${key}Unit`] = 'absolute';
            }
        });
        return result;
    }

    function preprocessMermaidDiagram(diagramText) {
        const lines = diagramText.split('\n');
        const map = {};
        let counter = 0;

        const processed = lines
            .map((line) => {
                if (!line.includes(':')) {
                    return line;
                }
                const idx = line.indexOf(':');
                const header = line.slice(0, idx);
                const message = line.slice(idx + 1);
                const match = message.match(/^\s*\([^)]*\)\s*(.*)$/);
                if (match) {
                    return line;
                }
                const directiveMatch = message.match(/^\s*\[([^\]]+)\]\s*(.*)$/);
                if (!directiveMatch) {
                    return line;
                }
                counter += 1;
                const directive = directiveMatch[1];
                const restText = directiveMatch[2] || '';
                const args = parseDirectiveStr(directive);
                const token = `__MSG_TOK_${Date.now()}_${counter}__`;
                map[token] = {
                    ...args,
                    orig: restText.trim(),
                };
                return `${header}: ${token}${restText ? ` ${restText}` : ''}`;
            })
            .join('\n');

        return { text: processed, map };
    }

    function clamp(value, min, max) {
        if (!Number.isFinite(value)) {
            return min;
        }
        return Math.min(Math.max(value, min), max);
    }

    function extractEndpointsFromPathD(d) {
        const nums = (d.match(/-?\d+\.?\d*/g) || []).map(Number);
        if (nums.length >= 4) {
            const x1 = nums[0];
            const y1 = nums[1];
            const x2 = nums[nums.length - 2];
            const y2 = nums[nums.length - 1];
            return { x1, y1, x2, y2 };
        }
        return null;
    }

    function makeSmoothCubicPath(x1, y1, x2, y2) {
        const dx = x2 - x1;
        const cp1x = x1 + dx * 0.25;
        const cp2x = x1 + dx * 0.75;
        const cp1y = y1 + (y2 - y1) * 0.15;
        const cp2y = y2 - (y2 - y1) * 0.15;
        return `M ${x1} ${y1} C ${cp1x} ${cp1y} ${cp2x} ${cp2y} ${x2} ${y2}`;
    }

    function findLabelElements(svgEl, token) {
        const texts = Array.from(svgEl.querySelectorAll('text')).filter((t) => (t.textContent || '').includes(token));
        const foreigns = Array.from(svgEl.querySelectorAll('foreignObject')).filter((f) => (f.textContent || '').includes(token));
        return texts.concat(foreigns);
    }

    function safeGetBBox(el) {
        try {
            return el.getBBox();
        } catch (error) {
            const rect = el.getBoundingClientRect();
            return {
                x: rect.x,
                y: rect.y,
                width: rect.width,
                height: rect.height,
            };
        }
    }

    async function postProcessMermaidSvg(svgEl, mapObj) {
        if (!mapObj || Object.keys(mapObj).length === 0) {
            return;
        }

        const allEdges = Array.from(svgEl.querySelectorAll('path, line'));
        let globalYMin = Infinity;
        let globalYMax = -Infinity;

        allEdges.forEach((edge) => {
            try {
                if (edge.tagName === 'line') {
                    const y1 = Number(edge.getAttribute('y1') || 0);
                    const y2 = Number(edge.getAttribute('y2') || 0);
                    globalYMin = Math.min(globalYMin, y1, y2);
                    globalYMax = Math.max(globalYMax, y1, y2);
                } else {
                    const pts = extractEndpointsFromPathD(edge.getAttribute('d') || '');
                    if (pts) {
                        globalYMin = Math.min(globalYMin, pts.y1, pts.y2);
                        globalYMax = Math.max(globalYMax, pts.y1, pts.y2);
                    }
                }
            } catch (error) {
                // ignore malformed paths
            }
        });

        if (!Number.isFinite(globalYMin) || !Number.isFinite(globalYMax)) {
            try {
                const bbox = svgEl.getBBox();
                globalYMin = bbox.y;
                globalYMax = bbox.y + bbox.height;
            } catch (error) {
                globalYMin = 0;
                globalYMax = 0;
            }
        }

        const actorNodes = Array.from(svgEl.querySelectorAll('[class*=actor]'));
        const actorBoxes = [];
        actorNodes.forEach((node) => {
            try {
                const box = node.getBBox();
                if (box && Number.isFinite(box.y) && Number.isFinite(box.height)) {
                    actorBoxes.push({ top: box.y, bottom: box.y + box.height, height: box.height });
                }
            } catch (error) {
                const rect = node.getBoundingClientRect?.();
                if (rect && Number.isFinite(rect.y) && Number.isFinite(rect.height)) {
                    actorBoxes.push({ top: rect.y, bottom: rect.y + rect.height, height: rect.height });
                }
            }
        });

        let percentBase = globalYMin;
        let percentRange = Math.max(1, globalYMax - globalYMin);

        if (actorBoxes.length > 0) {
            const minBottom = Math.min(...actorBoxes.map((box) => box.bottom));
            const maxTop = Math.max(...actorBoxes.map((box) => box.top));
            const avgHeight = actorBoxes.reduce((sum, box) => sum + box.height, 0) / actorBoxes.length;
            const dynamicPad = clamp(avgHeight * 0.3, 8, 40);
            const startCandidate = Math.max(globalYMin, minBottom + dynamicPad);
            const endCandidate = Math.min(globalYMax, maxTop - dynamicPad);

            if (Number.isFinite(startCandidate) && Number.isFinite(endCandidate) && endCandidate - startCandidate >= 4) {
                percentBase = startCandidate;
                percentRange = endCandidate - startCandidate;
            } else {
                const fallbackPad = clamp((globalYMax - globalYMin) * 0.05, 8, 40);
                const fallbackStart = Math.max(globalYMin, globalYMin + fallbackPad);
                const fallbackEnd = Math.max(fallbackStart + 1, globalYMax - fallbackPad);
                percentBase = fallbackStart;
                percentRange = fallbackEnd - fallbackStart;
            }
        }

        function resolveAbsolute(value, unit, fallback) {
            if (typeof value !== 'number') {
                return fallback;
            }
            if (unit === 'percent') {
                return percentBase + (value / 100) * percentRange;
            }
            return value;
        }

        function resolveDelta(value, unit) {
            if (typeof value !== 'number') {
                return 0;
            }
            if (unit === 'percent') {
                return (value / 100) * percentRange;
            }
            return value;
        }

        Object.keys(mapObj).forEach((token) => {
            const info = mapObj[token];
            const labelElements = findLabelElements(svgEl, token);
            if (labelElements.length === 0) {
                return;
            }

            labelElements.forEach((labelEl) => {
                let group = labelEl;
                while (group && group.nodeName !== 'g' && group.nodeName !== 'svg') {
                    group = group.parentNode;
                }
                const parent = group && group.parentNode;
                let candidates = parent ? Array.from(parent.querySelectorAll('path, line')) : [];
                if (candidates.length === 0) {
                    candidates = allEdges.slice();
                }

                const labelBox = safeGetBBox(labelEl);
                const labelCenter = {
                    x: labelBox.x + (labelBox.width || 0) / 2,
                    y: labelBox.y + (labelBox.height || 0) / 2,
                };

                let bestEdge = null;
                let bestDist = Infinity;
                candidates.forEach((edge) => {
                    try {
                        let center = { x: 0, y: 0 };
                        if (edge.tagName === 'line') {
                            const x1 = Number(edge.getAttribute('x1') || 0);
                            const y1 = Number(edge.getAttribute('y1') || 0);
                            const x2 = Number(edge.getAttribute('x2') || 0);
                            const y2 = Number(edge.getAttribute('y2') || 0);
                            center = { x: (x1 + x2) / 2, y: (y1 + y2) / 2 };
                        } else {
                            const pts = extractEndpointsFromPathD(edge.getAttribute('d') || '');
                            if (pts) {
                                center = { x: (pts.x1 + pts.x2) / 2, y: (pts.y1 + pts.y2) / 2 };
                            } else {
                                const bb = edge.getBBox();
                                center = { x: bb.x + bb.width / 2, y: bb.y + bb.height / 2 };
                            }
                        }
                        const dx = center.x - labelCenter.x;
                        const dy = center.y - labelCenter.y;
                        const dist = dx * dx + dy * dy;
                        if (dist < bestDist) {
                            bestDist = dist;
                            bestEdge = edge;
                        }
                    } catch (error) {
                        // ignore
                    }
                });

                if (!bestEdge) {
                    return;
                }

                let x1;
                let y1;
                let x2;
                let y2;
                if (bestEdge.tagName === 'line') {
                    x1 = Number(bestEdge.getAttribute('x1') || 0);
                    y1 = Number(bestEdge.getAttribute('y1') || 0);
                    x2 = Number(bestEdge.getAttribute('x2') || 0);
                    y2 = Number(bestEdge.getAttribute('y2') || 0);
                } else {
                    const pts = extractEndpointsFromPathD(bestEdge.getAttribute('d') || '');
                    if (!pts) {
                        return;
                    }
                    x1 = pts.x1;
                    y1 = pts.y1;
                    x2 = pts.x2;
                    y2 = pts.y2;
                }

                let newY1 = y1;
                let newY2 = y2;
                if (typeof info.ys === 'number') {
                    newY1 = resolveAbsolute(info.ys, info.ysUnit, newY1);
                } else if (typeof info.dys === 'number') {
                    newY1 += resolveDelta(info.dys, info.dysUnit);
                }
                if (typeof info.ye === 'number') {
                    newY2 = resolveAbsolute(info.ye, info.yeUnit, newY2);
                } else if (typeof info.dye === 'number') {
                    newY2 += resolveDelta(info.dye, info.dyeUnit);
                }

                if (bestEdge.tagName === 'line') {
                    const newPathD = makeSmoothCubicPath(x1, newY1, x2, newY2);
                    const svgNS = 'http://www.w3.org/2000/svg';
                    const newPath = document.createElementNS(svgNS, 'path');
                    Array.from(bestEdge.attributes).forEach((attr) => {
                        if (!['x1', 'y1', 'x2', 'y2'].includes(attr.name)) {
                            newPath.setAttribute(attr.name, attr.value);
                        }
                    });
                    newPath.setAttribute('d', newPathD);
                    bestEdge.parentNode.replaceChild(newPath, bestEdge);
                    bestEdge = newPath;
                } else {
                    const newPathD = makeSmoothCubicPath(x1, newY1, x2, newY2);
                    bestEdge.setAttribute('d', newPathD);
                }

                if (labelEl.textContent && labelEl.textContent.includes(token)) {
                    const replacement = (info.orig || '').trim();
                    labelEl.textContent = replacement ? replacement : labelEl.textContent.replace(token, '').trim();
                }

                const midX = (x1 + x2) / 2;
                const midY = (newY1 + newY2) / 2;
                let moveTarget = labelEl;
                if (labelEl.tagName && labelEl.tagName.toLowerCase() === 'tspan') {
                    moveTarget = labelEl.parentNode;
                }

                try {
                    const bbox = moveTarget.getBBox();
                    const currentCx = bbox.x + bbox.width / 2;
                    const currentCy = bbox.y + bbox.height / 2;
                    const dx = midX - currentCx;
                    const dy = midY - currentCy - 8;
                    const prevTransform = moveTarget.getAttribute('transform') || '';
                    moveTarget.setAttribute('transform', `${prevTransform} translate(${dx}, ${dy})`);
                } catch (error) {
                    moveTarget.style.transform = `translate(${midX}px, ${midY - 8}px)`;
                }
            });
        });
    }

    async function renderMermaidSequenceDiagram(container, timeline) {
        if (typeof mermaid === 'undefined') {
            container.innerHTML = '<div class="sequence-diagram-error">Mermaid.js library not loaded</div>';
            return;
        }

        container.innerHTML = '<div class="sequence-diagram-loading">Generating sequence diagram...</div>';

        let mermaidCode = '';
        try {
            mermaidCode = convertTimelineToMermaid(timeline);
            mermaid.initialize({
                startOnLoad: false,
                theme: 'default',
                sequence: {
                    diagramMarginX: 50,
                    diagramMarginY: 10,
                    actorMargin: 50,
                    width: 150,
                    height: 65,
                    boxMargin: 10,
                    boxTextMargin: 5,
                    noteMargin: 10,
                    messageMargin: 35,
                    mirrorActors: true,
                    bottomMarginAdj: 1,
                    useMaxWidth: true,
                    rightAngles: false,
                    showSequenceNumbers: false,
                },
            });

            const diagramId = `mermaid-diagram-${timeline.transactionID || Date.now()}`;
            const { text: processedCode, map } = preprocessMermaidDiagram(mermaidCode);
            const { svg } = await mermaid.render(diagramId, processedCode);
            const wrapper = document.createElement('div');
            wrapper.innerHTML = svg;
            const svgEl = wrapper.querySelector('svg');
            if (!svgEl) {
                container.innerHTML = '<div class="sequence-diagram-error">Mermaid rendered empty diagram</div>';
                return;
            }

            container.innerHTML = '';
            container.appendChild(svgEl);
            await postProcessMermaidSvg(svgEl, map);

            const sourceBlock = document.createElement('pre');
            sourceBlock.className = 'sequence-diagram-source';
            sourceBlock.textContent = mermaidCode;
            sourceBlock.style.marginTop = '12px';
            sourceBlock.style.padding = '8px 12px';
            sourceBlock.style.background = '#f7f7f7';
            sourceBlock.style.borderRadius = '6px';
            sourceBlock.style.fontSize = '11px';
            sourceBlock.style.lineHeight = '1.45';
            sourceBlock.style.whiteSpace = 'pre';
            sourceBlock.style.overflowX = 'auto';
            container.appendChild(sourceBlock);
        } catch (error) {
            console.error('Mermaid rendering error:', error);
            container.innerHTML = `<div class="sequence-diagram-error">Failed to render sequence diagram: ${error.message}<br><br>Mermaid Code:<br><pre style="font-size: 10px; overflow: auto;">${mermaidCode || 'N/A'}</pre></div>`;
        }
    }

    return {
        activate,
        deactivate,
        clear,
        loadTimeline,
    };
}
