const STATUS_COLORS = {
    info: '#666',
    success: '#389e0d',
    warn: '#d48806',
    error: '#c33',
};

const CONFIG_KEYS = [
    { key: 'EnablePacketHistory', label: 'Enable Packet History', format: (v) => String(v) },
    { key: 'MaxPacketHistorySize', label: 'Max Packet History Size', format: (v) => String(v) },
    { key: 'MaxTransactionHistory', label: 'Max Transaction History', format: (v) => String(v) },
    { key: 'HistoryOverflowMode', label: 'History Overflow Mode', format: (v) => v || 'circular' },
];

export function createPolicyView({
    panelElement,
    containerElement,
    statusElement,
    buttons,
}) {
    if (!panelElement || !containerElement || !buttons) {
        throw new Error('Policy view requires panel, container, and buttons.');
    }

    const state = {
        options: [],
        selected: new Set(),
        version: 0,
        updatedAt: null,
        loading: false,
        config: {},
    };

    buttons.refresh?.addEventListener('click', loadPolicies);
    buttons.apply?.addEventListener('click', applyPolicies);

    function activate() {
        loadPolicies();
    }

    function deactivate() {
        setStatus('');
    }

    function handleFrame() {
        // Policies are applied on reset; nothing to do per frame for now.
    }

    function loadPolicies() {
        if (state.loading) {
            return;
        }
        state.loading = true;
        setStatus('Loading policy configuration...', 'info');
        fetch('/api/policy')
            .then((response) => {
                if (!response.ok) {
                    throw new Error(`HTTP ${response.status}`);
                }
                return response.json();
            })
            .then((data) => {
                const incentives = data?.incentives || {};
                state.options = Array.isArray(incentives.available) ? incentives.available : [];
                state.selected = new Set(Array.isArray(incentives.selected) ? incentives.selected : []);
                state.version = incentives.version || 0;
                state.updatedAt = incentives.updatedAt || null;
                state.config = data?.config || {};
                renderView();
                setStatus('Policies loaded.', 'success');
            })
            .catch((error) => {
                console.error('Failed to load policies:', error);
                setStatus(`Failed to load policies: ${error.message}`, 'error');
            })
            .finally(() => {
                state.loading = false;
            });
    }

    function applyPolicies() {
        if (state.loading) {
            return;
        }
        state.loading = true;
        setStatus('Saving policy configuration...', 'info');
        const payload = {
            incentives: Array.from(state.selected.values()),
        };
        fetch('/api/policy', {
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
                const incentives = data?.incentives || {};
                state.options = Array.isArray(incentives.available) ? incentives.available : state.options;
                state.selected = new Set(Array.isArray(incentives.selected) ? incentives.selected : []);
                state.version = incentives.version || state.version;
                state.updatedAt = incentives.updatedAt || state.updatedAt;
                state.config = data?.config || state.config;
                renderView();
                setStatus('Policy draft saved. Apply by issuing a reset command.', 'success');
            })
            .catch((error) => {
                console.error('Failed to save policies:', error);
                setStatus(`Failed to save policies: ${error.message}`, 'error');
            })
            .finally(() => {
                state.loading = false;
            });
    }

    function renderView() {
        containerElement.innerHTML = '';
        containerElement.appendChild(renderConfigSection());
        containerElement.appendChild(renderPolicySection());
    }

    function renderConfigSection() {
        const wrapper = document.createElement('div');
        wrapper.style.marginBottom = '16px';
        const title = document.createElement('h3');
        title.textContent = 'Config & Policy';
        title.style.marginBottom = '8px';
        wrapper.appendChild(title);

        const table = document.createElement('table');
        table.style.width = '100%';
        table.style.borderCollapse = 'collapse';
        const tbody = document.createElement('tbody');

        CONFIG_KEYS.forEach(({ key, label, format }) => {
            const tr = document.createElement('tr');
            const nameTd = document.createElement('td');
            nameTd.textContent = label;
            nameTd.style.fontWeight = '600';
            nameTd.style.padding = '4px 8px';
            const valueTd = document.createElement('td');
            const raw = state.config[key];
            valueTd.textContent = format(raw);
            valueTd.style.padding = '4px 8px';
            valueTd.style.color = '#555';
            tr.appendChild(nameTd);
            tr.appendChild(valueTd);
            tbody.appendChild(tr);
        });

        table.appendChild(tbody);
        wrapper.appendChild(table);
        return wrapper;
    }

    function renderPolicySection() {
        const wrapper = document.createElement('div');
        const title = document.createElement('h3');
        title.textContent = 'Incentive Plugins';
        title.style.margin = '16px 0 8px';
        wrapper.appendChild(title);

        if (!state.options.length) {
            const empty = document.createElement('div');
            empty.className = 'empty-state';
            empty.textContent = 'No incentive plugins registered. Please check simulator configuration.';
            wrapper.appendChild(empty);
            return wrapper;
        }

        const list = document.createElement('div');
        list.style.display = 'flex';
        list.style.flexDirection = 'column';
        list.style.gap = '12px';

        state.options.forEach((option) => {
            const item = document.createElement('label');
            item.style.display = 'flex';
            item.style.alignItems = 'flex-start';
            item.style.gap = '8px';
            item.style.cursor = 'pointer';

            const checkbox = document.createElement('input');
            checkbox.type = 'checkbox';
            checkbox.checked = state.selected.has(option.name);
            checkbox.addEventListener('change', () => {
                if (checkbox.checked) {
                    state.selected.add(option.name);
                } else {
                    state.selected.delete(option.name);
                }
            });

            const content = document.createElement('div');
            const heading = document.createElement('div');
            heading.textContent = option.name;
            heading.style.fontWeight = '600';
            const desc = document.createElement('div');
            desc.textContent = option.description || 'No description provided.';
            desc.style.fontSize = '12px';
            desc.style.color = '#666';

            content.appendChild(heading);
            content.appendChild(desc);

            item.appendChild(checkbox);
            item.appendChild(content);
            list.appendChild(item);
        });

        wrapper.appendChild(list);
        return wrapper;
    }

    function setStatus(message, level = 'info') {
        if (!statusElement) {
            return;
        }
        statusElement.textContent = message || '';
        statusElement.style.color = STATUS_COLORS[level] || STATUS_COLORS.info;
    }

    return {
        activate,
        deactivate,
        handleFrame,
    };
}
