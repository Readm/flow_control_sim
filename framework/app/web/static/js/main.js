import { createFlowView } from "./views/flowView.js";
import { createTxnView } from "./views/txnView.js";
import { createTopologyView } from "./views/topologyView.js";
import { createPolicyView } from "./views/policyView.js";

const DEFAULT_VIEW_ID = "flowView";

const state = {
    isPaused: true,
    isRunProcessing: false,
    expectingReset: false,
    currentFrame: null,
    ws: null,
    reconnectTimer: null,
};

const controlButtons = {
    pause: document.getElementById("btnPause"),
    run: document.getElementById("btnRun"),
    reset: document.getElementById("btnReset"),
};

const runCycleInput = document.getElementById("runCycleCount");

const statusElements = {
    cycle: document.getElementById("currentCycle"),
    inFlight: document.getElementById("inFlightCount"),
    status: document.getElementById("simStatus"),
    error: document.getElementById("errorMessage"),
};

const configElements = {
    select: document.getElementById("networkConfig"),
    totalCycles: document.getElementById("totalCycles"),
};

const configTotals = new Map();

const statsPanel = document.getElementById("statsPanel");

const viewRegistry = new Map();
let activeViewId = DEFAULT_VIEW_ID;
let flowViewController;
let txnViewController;
let topologyViewController;
let policyViewController;

function initialize() {
    setupModalHandling();
    setupViews();
    setupTabs();
    setupControlPanel();
    setupConfigForm();
    loadConfigOptions();
    connectWebSocket();
    activateView(DEFAULT_VIEW_ID);
    updateButtonStates();
    window.addEventListener("beforeunload", disconnectWebSocket);
}

function setupViews() {
    const flowPanel = document.getElementById("flowView");
    const txnPanel = document.getElementById("txnView");
    const topologyPanel = document.getElementById("topologyView");
    const policyPanel = document.getElementById("policyView");

    flowViewController = createFlowView({
        panelElement: flowPanel,
        cyElement: document.getElementById("cy"),
        overlayElement: document.getElementById("pipelineOverlay"),
        loadingElement: document.getElementById("loading"),
        showPacketModal,
    });

    txnViewController = createTxnView({
        panelElement: txnPanel,
        containerElement: document.getElementById("sequenceDiagramContainer"),
        inputElement: document.getElementById("transactionID"),
        loadButton: document.getElementById("btnLoadTimeline"),
        errorElement: document.getElementById("timelineError"),
    });

    topologyViewController = createTopologyView({
        panelElement: topologyPanel,
        containerElement: document.getElementById("topologyEditor"),
        statusElement: document.getElementById("topologyStatus"),
        buttons: {
            addNode: document.getElementById("btnTopologyAddNode"),
            addLink: document.getElementById("btnTopologyAddLink"),
            delete: document.getElementById("btnTopologyDelete"),
            undo: document.getElementById("btnTopologyUndo"),
            redo: document.getElementById("btnTopologyRedo"),
            save: document.getElementById("btnTopologySave"),
            reload: document.getElementById("btnTopologyReload"),
        },
    });

    policyViewController = createPolicyView({
        panelElement: policyPanel,
        containerElement: document.getElementById("policyConfigurator"),
        statusElement: document.getElementById("policyStatus"),
        buttons: {
            refresh: document.getElementById("btnPolicyRefresh"),
            apply: document.getElementById("btnPolicyApply"),
        },
    });

    viewRegistry.set("flowView", { element: flowPanel, controller: flowViewController });
    viewRegistry.set("txnView", { element: txnPanel, controller: txnViewController });
    viewRegistry.set("topologyView", { element: topologyPanel, controller: topologyViewController });
    viewRegistry.set("policyView", { element: policyPanel, controller: policyViewController });
}

function createPassiveController(element) {
    return {
        activate() {
            if (element) {
                element.classList.add("active");
            }
        },
        deactivate() {
            if (element) {
                element.classList.remove("active");
            }
        },
    };
}

function setupTabs() {
    document.querySelectorAll("[data-view-target]").forEach((tab) => {
        tab.addEventListener("click", () => {
            const target = tab.getAttribute("data-view-target");
            if (target) {
                activateView(target);
            }
        });
    });
}

function activateView(targetId) {
    if (!viewRegistry.has(targetId) || activeViewId === targetId) {
        return;
    }

    const current = viewRegistry.get(activeViewId);
    if (current) {
        current.element.classList.remove("active");
        current.controller?.deactivate?.();
    }

    const next = viewRegistry.get(targetId);
    if (next) {
        next.element.classList.add("active");
        next.controller?.activate?.();
        updateTabHighlights(targetId);
        activeViewId = targetId;
    }
}

function updateTabHighlights(activeId) {
    document.querySelectorAll("[data-view-target]").forEach((tab) => {
        const match = tab.getAttribute("data-view-target") === activeId;
        tab.classList.toggle("active", match);
    });
}

function setupControlPanel() {
    controlButtons.pause?.addEventListener("click", () => handleControlCommand("pause"));
    controlButtons.run?.addEventListener("click", () => handleRunCommand());
    controlButtons.reset?.addEventListener("click", () => handleControlCommand("reset"));
}

function setupConfigForm() {
    configElements.select?.addEventListener("change", () => {
        updateTotalCyclesDisplay();
    });
}

async function handleControlCommand(type, options = {}) {
    clearErrorMessage();
    const button = getButtonByType(type);
    disableButton(button, true);

    let success = true;
    try {
        const payload = buildControlPayload(type, options);
        const response = await fetch("/api/control", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(payload),
        });

        if (!response.ok) {
            const errorText = await response.text();
            throw new Error(errorText || `Failed to execute ${type} command`);
        }

        applyControlState(type, options);
    } catch (error) {
        success = false;
        showErrorMessage(error.message || "Failed to send control command");
    } finally {
        disableButton(button, false);
        updateButtonStates();
    }

    return success;
}

async function handleRunCommand() {
    if (state.isRunProcessing) {
        return;
    }
    let cycles = 1;
    if (runCycleInput) {
        const parsed = parseInt(runCycleInput.value, 10);
        if (Number.isFinite(parsed)) {
            cycles = parsed;
        }
    }
    if (cycles <= 0) {
        showErrorMessage("Cycles must be a positive number");
        runCycleInput?.focus();
        return;
    }
    if (runCycleInput) {
        runCycleInput.value = String(cycles);
    }

    state.isRunProcessing = true;
    updateButtonStates();
    const success = await handleControlCommand("run", { cycles });

    try {
        if (success) {
            for (let attempt = 0; attempt < 5; attempt += 1) {
                const frame = await fetchFrameImmediate();
                if (frame) {
                    break;
                }
                await delay(100);
            }
        }
    } finally {
        state.isRunProcessing = false;
        updateButtonStates();
    }
}

function buildControlPayload(type, options = {}) {
    const payload = { type };
    if (type === "reset") {
        const configName = configElements.select?.value || "";
        if (configName) {
            payload.configName = configName;
        }
    } else if (type === "run") {
        payload.cycles = options.cycles || 1;
    }
    return payload;
}

function applyControlState(type) {
    switch (type) {
        case "pause":
            state.isPaused = true;
            updateStatusText("Paused");
            break;
        case "run":
            state.isPaused = false;
            updateStatusText("Running");
            break;
        case "reset":
            state.isPaused = true;
            state.expectingReset = true;
            flowViewController?.setExpectingReset(true);
            updateStatusText("Resetting");
            updateCycleValue("-");
            updateInFlightValue("-");
            break;
        default:
            break;
    }
}

function getButtonByType(type) {
    switch (type) {
        case "pause":
            return controlButtons.pause;
        case "run":
            return controlButtons.run;
        case "reset":
            return controlButtons.reset;
        default:
            return null;
    }
}

function disableButton(button, disabled) {
    if (button) {
        button.disabled = disabled;
    }
}

function updateButtonStates() {
    if (controlButtons.pause) {
        controlButtons.pause.disabled = state.isPaused || state.expectingReset;
    }
    if (controlButtons.run) {
        controlButtons.run.disabled = state.isRunProcessing || state.expectingReset;
    }
    if (controlButtons.reset) {
        controlButtons.reset.disabled = state.expectingReset;
    }
}

async function fetchFrameImmediate() {
    try {
        const response = await fetch("/api/frame");
        if (!response.ok) {
            return null;
        }
        const frame = await response.json();
        handleFrame(frame);
        return frame;
    } catch (error) {
        console.warn("Failed to fetch frame immediately", error);
        return null;
    }
}

function connectWebSocket() {
    disconnectWebSocket();

    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const url = `${protocol}//${window.location.host}/ws`;
    try {
        const ws = new WebSocket(url);
        state.ws = ws;

        ws.onopen = () => {
            clearErrorMessage();
        };

        ws.onmessage = (event) => {
            try {
                const frame = JSON.parse(event.data);
                handleFrame(frame);
            } catch (error) {
                console.error("Failed to parse frame", error);
            }
        };

        ws.onerror = () => {
            showErrorMessage("WebSocket connection error. Retrying...");
        };

        ws.onclose = () => {
            state.ws = null;
            if (state.reconnectTimer) {
                clearTimeout(state.reconnectTimer);
            }
            state.reconnectTimer = setTimeout(connectWebSocket, 2000);
        };
    } catch (error) {
        showErrorMessage("Unable to establish WebSocket connection.");
        console.error("WebSocket init failed", error);
    }
}

function disconnectWebSocket() {
    if (state.reconnectTimer) {
        clearTimeout(state.reconnectTimer);
        state.reconnectTimer = null;
    }
    if (state.ws) {
        state.ws.close();
        state.ws = null;
    }
}

function handleFrame(frame) {
    if (!frame) {
        return;
    }
    state.currentFrame = frame;

    if (typeof frame.paused === "boolean") {
        state.isPaused = frame.paused;
    }

    if (state.expectingReset && frame.cycle === 0) {
        state.expectingReset = false;
    }

    updateCycleValue(frame.cycle ?? "-");
    updateInFlightValue(frame.inFlightCount ?? "-");

    if (state.expectingReset) {
        updateStatusText("Resetting");
    } else if (state.isPaused) {
        updateStatusText("Paused");
    } else {
        updateStatusText("Running");
    }

    flowViewController?.updateFrame(frame);
    topologyViewController?.handleFrame(frame);
    policyViewController?.handleFrame?.(frame);
    updateStatisticsPanel(frame.stats);
    updateButtonStates();
}

function updateStatisticsPanel(stats) {
    if (!statsPanel) {
        return;
    }
    if (!stats || !stats.Global) {
        statsPanel.innerHTML = '<div class="stats-item"><div style="text-align: center; color: #999;">No data available</div></div>';
        return;
    }

    let html = '';
    const global = stats.Global;
    html += '<div class="stats-item">';
    html += '<h4>Global Statistics</h4>';
    html += '<div class="stats-grid">';
    html += `<div class="stats-value">Total Requests: <strong>${global.TotalRequests}</strong></div>`;
    html += `<div class="stats-value">Completed: <strong>${global.Completed}</strong></div>`;
    html += `<div class="stats-value">Completion Rate: <strong>${global.CompletionRate.toFixed(2)}%</strong></div>`;
    html += `<div class="stats-value">Avg Delay: <strong>${global.AvgEndToEndDelay.toFixed(2)} cy</strong></div>`;
    html += `<div class="stats-value">Max Delay: <strong>${global.MaxDelay} cy</strong></div>`;
    html += `<div class="stats-value">Min Delay: <strong>${global.MinDelay} cy</strong></div>`;
    html += '</div></div>';

    if (Array.isArray(stats.PerMaster) && stats.PerMaster.length > 0) {
        html += '<div class="stats-item"><h4>Master Statistics</h4>';
        stats.PerMaster.forEach((entry, idx) => {
            if (!entry) {
                return;
            }
            html += `<div style="margin-top: 6px; padding-top: 6px; border-top: 1px solid #eee;">`;
            html += `<strong>Master ${idx}</strong><br>`;
            html += `Completed: ${entry.CompletedRequests}, `;
            html += `Avg Delay: ${entry.AvgDelay.toFixed(2)} cy, `;
            html += `Max: ${entry.MaxDelay} cy, Min: ${entry.MinDelay} cy`;
            html += '</div>';
        });
        html += '</div>';
    }

    if (Array.isArray(stats.PerSlave) && stats.PerSlave.length > 0) {
        html += '<div class="stats-item"><h4>Slave Statistics</h4>';
        stats.PerSlave.forEach((entry, idx) => {
            if (!entry) {
                return;
            }
            html += `<div style="margin-top: 6px; padding-top: 6px; border-top: 1px solid #eee;">`;
            html += `<strong>Slave ${idx}</strong><br>`;
            html += `Processed: ${entry.TotalProcessed}, `;
            html += `Max Queue: ${entry.MaxQueueLength}, `;
            html += `Avg Queue: ${entry.AvgQueueLength.toFixed(2)}`;
            html += '</div>';
        });
        html += '</div>';
    }

    statsPanel.innerHTML = html;
}

async function loadConfigOptions() {
    if (!configElements.select) {
        return;
    }
    try {
        const response = await fetch("/api/configs");
        if (!response.ok) {
            throw new Error(`Failed to load configs (${response.status})`);
        }
        const configs = await response.json();
        populateConfigSelect(configs);
    } catch (error) {
        populateConfigSelect([]);
        showErrorMessage(error.message || "Failed to fetch configuration list");
    }
}

function populateConfigSelect(configs) {
    const select = configElements.select;
    if (!select) {
        return;
    }
    const previousValue = select.value;
    select.innerHTML = "";
    configTotals.clear();
    if (!configs || configs.length === 0) {
        const option = document.createElement("option");
        option.value = "";
        option.textContent = "No configurations available";
        select.appendChild(option);
        select.disabled = true;
        updateTotalCyclesDisplay();
        return;
    }
    select.disabled = false;
    configs.forEach((cfg) => {
        const option = document.createElement("option");
        option.value = cfg.name || "";
        option.textContent = cfg.description ? `${cfg.name} – ${cfg.description}` : cfg.name;
        select.appendChild(option);
        if (option.value) {
            const total = Number(cfg.totalCycles);
            configTotals.set(option.value, Number.isFinite(total) ? total : null);
        }
    });
    if (previousValue && configTotals.has(previousValue)) {
        select.value = previousValue;
    } else if (select.options.length > 0) {
        select.selectedIndex = 0;
    }
    updateTotalCyclesDisplay();
}

function updateTotalCyclesDisplay() {
    if (!configElements.totalCycles) {
        return;
    }
    const selected = configElements.select?.value || "";
    const total = configTotals.get(selected);
    if (typeof total === "number" && Number.isFinite(total) && total > 0) {
        configElements.totalCycles.textContent = total;
    } else {
        configElements.totalCycles.textContent = "-";
    }
}

function showPacketModal(modalData) {
    const modal = document.getElementById("packetModal");
    const title = document.getElementById("modalTitle");
    const body = document.getElementById("modalBody");
    if (!modal || !title || !body) {
        return;
    }
    title.textContent = modalData.title;
    body.innerHTML = modalData.html;
    modal.classList.add("show");
}

function setupModalHandling() {
    const modal = document.getElementById("packetModal");
    const closeBtn = document.getElementById("modalClose");
    if (!modal || !closeBtn) {
        return;
    }
    closeBtn.addEventListener("click", () => modal.classList.remove("show"));
    modal.addEventListener("click", (event) => {
        if (event.target === modal) {
            modal.classList.remove("show");
        }
    });
}

function updateStatusText(text) {
    if (statusElements.status) {
        statusElements.status.innerHTML = `Status: <strong>${text}</strong>`;
    }
}

function updateCycleValue(value) {
    if (statusElements.cycle) {
        statusElements.cycle.textContent = value;
    }
}

function updateInFlightValue(value) {
    if (statusElements.inFlight) {
        statusElements.inFlight.textContent = value;
    }
}

function showErrorMessage(message) {
    if (statusElements.error) {
        statusElements.error.textContent = message;
        statusElements.error.classList.add("show");
    }
}

function clearErrorMessage() {
    if (statusElements.error) {
        statusElements.error.textContent = "";
        statusElements.error.classList.remove("show");
    }
}

function delay(ms) {
    return new Promise((resolve) => setTimeout(resolve, ms));
}

if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", initialize);
} else {
    initialize();
}

