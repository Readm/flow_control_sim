# 在 Mermaid 时序图中：用「预处理指令 + 渲染后处理」实现按纵坐标与斜向消息（Markdown 指南）

下面的 Markdown 文档说明并示例了 **一种工程化、可复用的魔改方案**：在用户的时序图消息前写**方括号指令**（例如 `[ys=260,ye=140]` 或 `[dys=+30,dye=-10]`），前端在把文本交给 Mermaid 渲染前**预处理**这些指令（替换为 token 并保存映射），渲染后再**后处理 SVG**（找到对应的箭头 path/line，修改端点 y 值并把标签移动到合适位置），从而实现精确的纵向控制与斜向消息线。此方案不改 Mermaid 源码、易集成、适合快速 PoC 与编辑器集成。

---

## 目录

1. 概述（为什么用这条路）
2. 指令语法
3. 实现原理（高层）
4. 最简测试：无魔改的基础 Mermaid 时序图（用于排查）
5. 完整魔改示例（可直接运行的 HTML）
6. 限制与注意事项
7. 可扩展方向与建议

---

# 1. 概述（为什么用这条路）

* **目的**：在 Mermaid 的时序图（sequenceDiagram）中，支持给单条消息指定纵向坐标（起点、终点）或相对偏移，并渲染成“斜向”消息线或平滑曲线。
* **优点**：

  * 无需修改 Mermaid 源码（兼容性好、快速迭代）。
  * 可在现有应用/编辑器中以插件式集成（pre → mermaid.render → post）。
  * 用户可通过简单指令声明视觉需求（可读、可自动化）。
* **缺点**：

  * 依赖 Mermaid 输出的 SVG 结构（不同版本可能需要微调选择器或匹配逻辑）。
  * 后处理为 DOM 操作，需维护和测试。

---

# 2. 指令语法（建议）

在消息前使用方括号写参数（逗号分隔），支持绝对、相对与百分比三类参数：

* 绝对坐标：

  * `ys=<number>`：起点 y（像素，SVG 坐标）
  * `ye=<number>`：终点 y

* 相对偏移（基于 Mermaid 原始端点 y）：

  * `dys=<number>`：起点 y 的偏移（正为下移，负为上移）
  * `dye=<number>`：终点 y 的偏移

* 百分比（基于整个消息区域的纵向比例）：

  * `ys=<number>%`：起点位于最小/最大 cycle 映射后的百分比位置
  * `ye=<number>%`：终点位于最小/最大 cycle 映射后的百分比位置
  * `dys=<number>%` / `dye=<number>%`：相对偏移的百分比写法（基于消息区域高度）

示例：

```
A->>B: [ys=260,ye=140] 斜向消息（绝对）
A->>B: [dys=+30,dye=-10] 斜向消息（相对）
A->>B: [ys=300] 只指定起点
```

---

# 3. 实现原理（高层）

1. **预处理（preprocess）**

   * 扫描 Mermaid 文本，识别消息前的方括号指令。
   * 为每条带指令的消息生成唯一 token（如 `__MSG_TOK_...__`），把原消息替换为 `token + 原文`，并把解析出的参数保存在映射表 `token -> {ys, ye, dys, dye, orig}`。

2. **渲染（mermaid.render）**

   * 把预处理后的文本交给 Mermaid 渲染，得到 SVG 字符串 / DOM。

3. **后处理（postprocess）**

   * 将 SVG 插入 DOM，找到包含 token 的 `<text>` / `foreignObject` 节点（标签）。
   * 根据标签的位置回溯或匹配到对应的 path/line（消息箭头）。
   * 解析 path 的端点（或读取 line 的 x1,y1,x2,y2），按参数计算新的 y 值（绝对或相对），用简单直线或平滑贝塞尔曲线替换 path 内容，并保留 marker（箭头）。
   * 把 token 替换回原始消息文本（或设置为用户可见文本），并将标签移动到曲线中点附近（使用 transform）。
   * 若指令给出百分比，先在渲染完成的 SVG 中统计所有消息线的最小/最大 y，再将百分比换算成实际像素，确保不同尺寸与缩放下仍能保留 cycle 之间的相对间距。

---

# 4. 最简测试：基础 Mermaid 时序图（无魔改）

在调试时，先确认 Mermaid 本身可正常渲染。把下面保存为 `base.html` 并打开，看是否有图。

```html
<!doctype html>
<html>
<head>
  <meta charset="utf-8" />
  <title>Mermaid 基础时序图测试</title>
  <script src="https://cdn.jsdelivr.net/npm/mermaid@10/dist/mermaid.min.js"></script>
</head>
<body>
  <div id="out"></div>
  <script>
    mermaid.initialize({ startOnLoad: false, securityLevel: 'loose' });
    const baseDiagram = `
sequenceDiagram
  participant A
  participant B
  A->>B: Hello B
  B-->>A: Hi A
`;
    (async function(){
      const { svg } = await mermaid.render('base', baseDiagram);
      document.getElementById('out').innerHTML = svg;
    })();
  </script>
</body>
</html>
```

如果这一文件能正常显示图形（参与者与箭头），说明 Mermaid 环境正常，可继续魔改。

---

# 5. 完整魔改示例（可直接运行的 HTML）

下面是**完整的端到端实现**（预处理 + 渲染 + 后处理、支持绝对/相对参数，并用贝塞尔曲线平滑）。
保存为 `magic_seq.html` 并在浏览器打开即可试验。

> **注意**：示例使用 `https://cdn.jsdelivr.net/npm/mermaid@10/dist/mermaid.min.js`，如果你在受限网络环境，建议下载 `mermaid.min.js` 放在本地并改为相对引用。

```html
<!doctype html>
<html>
<head>
  <meta charset="utf-8" />
  <title>Mermaid 魔改：预处理指令 + 后处理（斜向 / 指定纵坐标）</title>
  <script src="https://cdn.jsdelivr.net/npm/mermaid@10/dist/mermaid.min.js"></script>
  <style>
    body { font-family: Inter, system-ui, -apple-system, "Segoe UI", Roboto; padding: 18px; }
    #out { border: 1px solid #e6e6e6; padding: 12px; max-width: 980px; overflow:auto; }
    pre.sample { background:#f7f7f7; padding:10px; border-radius:6px; }
  </style>
</head>
<body>
  <h2>Mermaid 魔改（预处理指令 + 后处理）</h2>
  <p>示例指令： <code>[ys=260,ye=140]</code> 或 <code>[dys=+30,dye=-10]</code></p>

  <div id="out"></div>

  <pre class="sample" id="sampleInput">
sequenceDiagram
  participant A
  participant B
  participant C

  A->>B: [ys=260,ye=140] A 到 B（绝对）
  B-->>C: 普通消息
  A->>C: [dys=+40,dye=-20] A 到 C（相对）
  C->>A: [ys=300] C 到 A（只指定起点）
  A->>B: [dye=-30] A 到 B（只指定终点偏移）
  </pre>

<script>
mermaid.initialize({ startOnLoad: false, securityLevel: 'loose' });

function parseDirectiveStr(s) {
  const out = {};
  const parts = s.split(',').map(p => p.trim()).filter(Boolean);
  for (const p of parts) {
    const m = p.match(/^([a-zA-Z]+)\s*=\s*([+-]?\d+(\.\d+)?)$/);
    if (m) out[m[1]] = Number(m[2]);
  }
  return out;
}

function preprocessDiagram(diagramText) {
  const lines = diagramText.split('\n');
  const map = {};
  let counter = 0;

  const processed = lines.map(line => {
    if (!line.includes(':')) return line;
    const idx = line.indexOf(':');
    const header = line.slice(0, idx);
    let message = line.slice(idx+1);
    const m = message.match(/^\s*\[([^\]]+)\]\s*(.*)$/);
    if (!m) return line;
    counter++;
    const directive = m[1];
    const restText = m[2] || '';
    const args = parseDirectiveStr(directive);
    const token = `__MSG_TOK_${Date.now()}_${counter}__`;
    map[token] = { ...args, orig: restText.trim() };
    return `${header}: ${token} ${restText}`;
  }).join('\n');

  return { text: processed, map };
}

async function renderAndPostprocess(diagramText, containerEl) {
  const { text: processedText, map } = preprocessDiagram(diagramText);
  try {
    const result = await mermaid.render('m_' + Date.now(), processedText);
    const wrapper = document.createElement('div');
    wrapper.innerHTML = result.svg;
    const svgEl = wrapper.querySelector('svg');
    if (!svgEl) {
      containerEl.innerHTML = '<pre>渲染失败：未生成 SVG</pre>';
      return;
    }
    containerEl.innerHTML = '';
    containerEl.appendChild(svgEl);
    await postProcessSVG(svgEl, map);
  } catch (err) {
    containerEl.innerHTML = `<pre>Mermaid 渲染出错：\n${err?.message || String(err)}</pre>`;
  }
}

function extractEndpointsFromPathD(d) {
  const nums = (d.match(/-?\d+\.?\d*/g) || []).map(Number);
  if (nums.length >= 4) {
    const x1 = nums[0], y1 = nums[1];
    const x2 = nums[nums.length-2], y2 = nums[nums.length-1];
    return { x1, y1, x2, y2 };
  }
  return null;
}

function makeSmoothCubicPath(x1,y1,x2,y2) {
  const dx = (x2 - x1);
  const cp1x = x1 + dx * 0.25;
  const cp2x = x1 + dx * 0.75;
  const cp1y = y1 + (y2 - y1) * 0.15;
  const cp2y = y2 - (y2 - y1) * 0.15;
  return `M ${x1} ${y1} C ${cp1x} ${cp1y} ${cp2x} ${cp2y} ${x2} ${y2}`;
}

function findLabelElements(svgEl, token) {
  const texts = Array.from(svgEl.querySelectorAll('text')).filter(t => (t.textContent||'').includes(token));
  const foreigns = Array.from(svgEl.querySelectorAll('foreignObject')).filter(f => (f.textContent||'').includes(token));
  return texts.concat(foreigns);
}

function safeGetBBox(el) {
  try {
    return el.getBBox();
  } catch (e) {
    const r = el.getBoundingClientRect();
    return { x: r.x, y: r.y, width: r.width, height: r.height };
  }
}

async function postProcessSVG(svgEl, mapObj) {
  const allEdges = Array.from(svgEl.querySelectorAll('path, line'));
  for (const token of Object.keys(mapObj)) {
    const info = mapObj[token];
    const labelEls = findLabelElements(svgEl, token);
    if (labelEls.length === 0) { console.warn('未找到 token 标签：', token); continue; }

    for (const labelEl of labelEls) {
      let group = labelEl;
      while (group && group.nodeName !== 'g' && group.nodeName !== 'svg') group = group.parentNode;
      const parent = group && group.parentNode;
      let candidates = parent ? Array.from(parent.querySelectorAll('path, line')) : [];
      if (candidates.length === 0) candidates = allEdges.slice();

      const lblBox = safeGetBBox(labelEl);
      const labelCenter = { x: (lblBox.x + (lblBox.width||0)/2), y: (lblBox.y + (lblBox.height||0)/2) };

      let best = null; let bestDist = Infinity;
      for (const c of candidates) {
        let ccenter = { x: 0, y: 0 };
        try {
          if (c.tagName === 'line') {
            const x1 = Number(c.getAttribute('x1')||0), y1 = Number(c.getAttribute('y1')||0);
            const x2 = Number(c.getAttribute('x2')||0), y2 = Number(c.getAttribute('y2')||0);
            ccenter.x = (x1 + x2)/2; ccenter.y = (y1 + y2)/2;
          } else {
            const d = c.getAttribute('d') || '';
            const pts = extractEndpointsFromPathD(d);
            if (pts) ccenter.x = (pts.x1 + pts.x2)/2, ccenter.y = (pts.y1 + pts.y2)/2;
            else { const bb = c.getBBox(); ccenter.x = bb.x + bb.width/2; ccenter.y = bb.y + bb.height/2; }
          }
        } catch (e) { continue; }
        const dx = ccenter.x - labelCenter.x; const dy = ccenter.y - labelCenter.y;
        const dist2 = dx*dx + dy*dy;
        if (dist2 < bestDist) { bestDist = dist2; best = c; }
      }

      if (!best) { console.warn('未找到对应 edge for token', token); continue; }
      const pathEl = best;

      let x1,y1,x2,y2;
      if (pathEl.tagName === 'line') {
        x1 = Number(pathEl.getAttribute('x1')||0); y1 = Number(pathEl.getAttribute('y1')||0);
        x2 = Number(pathEl.getAttribute('x2')||0); y2 = Number(pathEl.getAttribute('y2')||0);
      } else {
        const d = pathEl.getAttribute('d') || '';
        const pts = extractEndpointsFromPathD(d);
        if (!pts) { console.warn('无法从 path 提取端点', pathEl); continue; }
        x1 = pts.x1; y1 = pts.y1; x2 = pts.x2; y2 = pts.y2;
      }

      let newY1 = y1, newY2 = y2;
      if (typeof info.ys === 'number') newY1 = info.ys;
      if (typeof info.ye === 'number') newY2 = info.ye;
      if (typeof info.dys === 'number') newY1 = y1 + info.dys;
      if (typeof info.dye === 'number') newY2 = y2 + info.dye;

      if (pathEl.tagName === 'line') {
        const newD = makeSmoothCubicPath(x1, newY1, x2, newY2);
        const svgNS = 'http://www.w3.org/2000/svg';
        const newPath = document.createElementNS(svgNS, 'path');
        Array.from(pathEl.attributes).forEach(attr => {
          if (['x1','y1','x2','y2'].includes(attr.name)) return;
          newPath.setAttribute(attr.name, attr.value);
        });
        newPath.setAttribute('d', newD);
        pathEl.parentNode.replaceChild(newPath, pathEl);
      } else {
        const newD = makeSmoothCubicPath(x1, newY1, x2, newY2);
        pathEl.setAttribute('d', newD);
      }

      try {
        if (labelEl.textContent && labelEl.textContent.includes(token)) {
          const newText = (mapObj[token].orig || '').trim();
          labelEl.textContent = newText || labelEl.textContent.replace(token, '').trim();
        }
      } catch (e) {}

      const midX = (x1 + x2) / 2;
      const midY = (newY1 + newY2) / 2;
      let moveTarget = labelEl;
      if (labelEl.tagName.toLowerCase() === 'tspan') moveTarget = labelEl.parentNode;

      try {
        const lb = moveTarget.getBBox();
        const curCx = lb.x + lb.width/2;
        const curCy = lb.y + lb.height/2;
        const dx = midX - curCx;
        const dy = midY - curCy - 8;
        const prev = moveTarget.getAttribute('transform') || '';
        moveTarget.setAttribute('transform', `${prev} translate(${dx}, ${dy})`);
      } catch (e) {
        moveTarget.style.transform = `translate(${midX}px, ${midY - 8}px)`;
      }
    }
  }
}

/* demo run */
const sampleInput = document.getElementById('sampleInput');
const out = document.getElementById('out');
renderAndPostprocess(sampleInput.textContent, out);
</script>
</body>
</html>
```

---

# 6. 限制与注意事项

* **依赖 Mermaid 输出结构**：不同版本的 Mermaid 可能改变 SVG 的类名、元素层级或标签表达方式（`foreignObject` vs `text` 等）。后处理的选择器和定位逻辑需根据你实际使用的 Mermaid 版本微调。
* **坐标系注意**：SVG 的 y 轴朝下为正，绝对值以像素为单位；如果你想使用百分比或相对基线（例如“participant 的位置”），需要在预处理或后处理中做转换。
* **AR/Accessibility**：直接修改 SVG 可能影响 aria 属性或可访问性标签。确保在后处理时保留或补上 `title`/`desc`/`aria` 信息。
* **性能**：大量消息的后处理会对 DOM 操作有性能影响；考虑批处理或只处理带指令的消息。
* **兼容性**：尽量保持对未带指令的消息无侵入（后处理仅作用于 token 对应的消息）。

---

# 7. 可扩展方向（建议）

* 支持 **百分比**（例如 `ys=30%`）或相对于 **participant** 的定位（计算目标参与者 x,y 并基于其坐标设置端点）。
* 支持 **控制点参数**，允许用户自定义贝塞尔弧度（如 `c1=...`）。
* 在编辑器中提供 **拖拽交互**：用户拖动消息线时动态更新 `dys/dye` 并写回源文本。
* 把后处理逻辑封装为小型库（`mermaid-message-offsets`），并在多平台（VSCode、MkDocs、Docusaurus）集成为插件。
* 如果计划长期维护、广泛使用：把语法加入 Mermaid parser（Langium）并在 sequence renderer 中原生支持该属性（需要走 PR 流程）。


