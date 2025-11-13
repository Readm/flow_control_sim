const STATUS_COLORS = {
    info: '#666',
    success: '#389e0d',
    warn: '#d48806',
    error: '#c33',
};

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
                renderOptions();
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
                renderOptions();
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

    function renderOptions() {
        containerElement.innerHTML = '';
        if (!state.options.length) {
            const empty = document.createElement('div');
            empty.className = 'empty-state';
            empty.textContent = 'No incentive plugins registered. Please check simulator configuration.';
            containerElement.appendChild(empty);
            return;
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
            const title = document.createElement('div');
            title.textContent = option.name;
            title.style.fontWeight = '600';
            const desc = document.createElement('div');
            desc.textContent = option.description || 'No description provided.';
            desc.style.fontSize = '12px';
            desc.style.color = '#666';

            content.appendChild(title);
            content.appendChild(desc);

            item.appendChild(checkbox);
            item.appendChild(content);
            list.appendChild(item);
        });

        containerElement.appendChild(list);
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
