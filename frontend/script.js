const API_BASE_URL = window.STRUKTURBILD_API_URL || "http://localhost:3000";
const IS_STORY_MODE = window.location.pathname.startsWith("/stories/");

document.addEventListener("DOMContentLoaded", () => {
  if (IS_STORY_MODE) {
    document.body.classList.add("story-mode");
    return;
  }
  const loadBtn = document.getElementById("loadPersonBtn");
  const createBtn = document.getElementById("createPersonBtn");
  const personInput = document.getElementById("personIdInput");

  // --- Dynamic layout tuning (adds a small slider into the UI) ---
  // Single "Spacing" slider controls COSE-like spacing when used, and spacing inside deterministic layouts.
  let layoutStrength = 65; // 0..100 (default 65 = roomier)
  function lerp(a, b, t) { return a + (b - a) * t; }
  function getCoseOptions() {
    const t = Math.max(0, Math.min(1, layoutStrength / 100)); // normalize 0..1
    const nodeRepulsion = Math.round(lerp(2e5, 2e6, t));
    const idealEdgeLength = Math.round(lerp(90, 260, t));
    const componentSpacing = Math.round(lerp(60, 220, t));
    return {
      name: 'cose',
      animate: false,
      fit: true,
      padding: 30,
      randomize: true,
      componentSpacing,
      nodeRepulsion,
      idealEdgeLength,
      edgeElasticity: 0.1,
      nestingFactor: 1.2,
      gravity: 80,
      initialTemp: 200,
      coolingFactor: 0.95,
      minTemp: 1.0
    };
  }

  // Create a small slider UI without touching index.html
  (function injectLayoutSlider(){
    const topBar = document.getElementById('top-bar') || document.body;
    const wrap = document.createElement('div');
    wrap.id = 'layout-tuner';
    wrap.style.display = 'inline-flex';
    wrap.style.alignItems = 'center';
    wrap.style.gap = '6px';
    wrap.style.marginLeft = '8px';
    wrap.style.userSelect = 'none';
    wrap.style.fontSize = '12px';
    wrap.style.padding = '2px 6px';
    wrap.style.borderRadius = '6px';
    wrap.style.border = '1px solid rgba(0,0,0,0.08)';
    wrap.style.background = 'rgba(255,255,255,0.6)';
    wrap.style.backdropFilter = 'blur(2px)';
    wrap.innerHTML = `
      <span title="Graph spacing">Spacing</span>
      <input id="spacingSlider" type="range" min="0" max="100" step="1" value="${layoutStrength}" style="width:120px;">
      <button id="spacingRelayoutBtn" type="button" title="Re-run layout">↺</button>
    `;
    if (topBar && topBar.appendChild) topBar.appendChild(wrap);
    const slider = wrap.querySelector('#spacingSlider');
    const relayoutBtn = wrap.querySelector('#spacingRelayoutBtn');
    let sliderDebounce;
    slider.addEventListener('input', () => {
      layoutStrength = parseInt(slider.value || '65', 10);
      clearTimeout(sliderDebounce);
      sliderDebounce = setTimeout(() => {
        needsLayout = true;
        reRender();
      }, 120);
    });
    relayoutBtn.addEventListener('click', () => {
      needsLayout = true;
      reRender();
    });
  })();
  // --- end dynamic layout tuning ---

  let cy;
  // Track last container size to avoid unnecessary resizes/layouts
  let lastContainerW = 0;
  let lastContainerH = 0;
  // Debounce flag for reRender
  let reRenderPending = false;
  let needsLayout = false;

  // In-memory dataset + UI state
  let lastNodes = [];
  let lastEdges = [];
  let currentFilter = 'schulentwicklungsziel'; // default to Schulentwicklungsziele on load
  let prevFilterBeforeExpand = null;           // remember filter while expanded
  let expandedNodeId = null;                   // node id if expanded (focus)

  // UX tuning
  const DOUBLE_TAP_MS = 350; // two taps within this window = expand/collapse
  let lastTapTime = 0;
  let lastTapNodeId = null;
  const DEBUG_STATUS = false; // write #status only when true (reduces scroll flicker)

  // === Taxonomy + colors ===
  const TYPE_COLORS = {
    'bedürfnis': '#a855f7',
    'trigger': '#f59e0b',
    'schulentwicklungsziel': '#111827',
    'barriere': '#ef4444',
    'promotor': '#22c55e',
    'outcome': '#3b82f6'
  };
  function stripDiacritics(s){
    try { return s.normalize('NFD').replace(/\p{Diacritic}/gu, ''); } catch { return s || ''; }
  }
  function toCanonicalType(t){
    const raw = (t ?? '').toString().trim().toLowerCase();
    const ascii = stripDiacritics(raw);
    const ALIASES = {
      // legacy -> new
      'promoter': 'promotor',
      'barrier': 'barriere',
      'goal': 'outcome',
      'event': 'trigger',
      // taxonomy rename + plural
      'entwicklungsinhalt': 'schulentwicklungsziel',
      'schulentwicklungsziele': 'schulentwicklungsziel',
      // diacritic/typing variants
      'bedurfnis': 'bedürfnis',
      'beduerfnis': 'bedürfnis'
    };
    return ALIASES[raw] || ALIASES[ascii] || raw;
  }

  let addNodeMode = false;
  const inspector = document.getElementById("inspector");
  const inspectorForm = document.getElementById("inspectorForm");
  const selTypeSpan = document.getElementById("inspectorSelectedType");
  const fieldLabel = document.getElementById("fieldLabel");
  const fieldType = document.getElementById("fieldType");
  const fieldTime = document.getElementById("fieldTime");
  const fieldColor = document.getElementById("fieldColor");
  const fieldDetail = document.getElementById("fieldDetail");
  const saveInspectorBtn = document.getElementById("saveInspectorBtn");
  const cancelInspectorBtn = document.getElementById("cancelInspectorBtn");
  const addNodeBtn = document.getElementById("addNodeBtn");
  const deleteSelectedBtn = document.getElementById("deleteSelectedBtn");
  const layoutSelect = document.getElementById("layoutSelect");
  const saveLayoutBtn = document.getElementById("saveLayoutBtn");
  const connectSelectedBtn = document.getElementById("connectSelectedBtn");
  let inspectorSelection = null; // cy element currently edited
  const filterTypeSelect = document.getElementById("filterTypeSelect");
  const clearExpandBtn = document.getElementById("clearExpandBtn"); // optional

  // Ensure "Compass" layout exists in the dropdown
  if (layoutSelect && !Array.from(layoutSelect.options).some(o => o.value === 'compass')) {
    const opt = document.createElement('option');
    opt.value = 'compass';
    opt.textContent = 'compass';
    layoutSelect.appendChild(opt);
  }

  // ==== Layout helpers: deterministic + packing ====

  // Pack nodes into a non-overlapping grid inside a rectangle
  // rect = { x0, y0, x1, y1 }
  function packIntoRect(arr, rect, minGap = 20) {
    const n = arr.length; if (!n) return;
    const NODE_W = 220, NODE_H = 90;
    const width = Math.max(1, rect.x1 - rect.x0);
    const height = Math.max(1, rect.y1 - rect.y0);

    // Determine max feasible columns with at least minGap between cells
    let cols = Math.max(1, Math.min(n, Math.floor((width + minGap) / (NODE_W + minGap))));
    let rows = Math.ceil(n / cols);

    // If too tall, reduce columns until it fits or we hit 1 col
    while (cols > 1 && (rows * NODE_H + (rows - 1) * minGap) > height) {
      cols -= 1; rows = Math.ceil(n / cols);
    }

    // If still tall with 1 col, reduce gap down to 6px (last resort)
    let vgap = (rows > 1) ? Math.floor((height - rows * NODE_H) / (rows - 1)) : 0;
    if (vgap < minGap) vgap = Math.max(6, vgap);

    let hgap = (cols > 1) ? Math.floor((width - cols * NODE_W) / (cols - 1)) : 0;
    if (hgap < minGap) hgap = Math.max(6, hgap);

    const gridW = cols * NODE_W + (cols - 1) * hgap;
    const gridH = rows * NODE_H + (rows - 1) * vgap;
    const startX = Math.round(rect.x0 + (width - gridW) / 2 + NODE_W / 2);
    const startY = Math.round(rect.y0 + (height - gridH) / 2 + NODE_H / 2);

    for (let i = 0; i < n; i++) {
      const c = i % cols; const r = Math.floor(i / cols);
      const x = startX + c * (NODE_W + hgap);
      const y = startY + r * (NODE_H + vgap);
      arr[i].position({ x, y });
    }
  }

  // Deterministic compass layout with disjoint rectangles per sector
  function layoutCompass(spacing = 1.0) {
    if (!cy) return;
    const cont = document.getElementById('cy');
    const W = Math.max(800, (cont?.clientWidth || 1000));
    const H = Math.max(600, (cont?.clientHeight || 700));

    // Global margins & gutters
    const M = 24; // outer margin
    const gW = Math.max(24, Math.round(W * 0.02));
    const gH = Math.max(24, Math.round(H * 0.02));

    // Fixed bands to guarantee no overlap between sectors
    const sideW = Math.round(W * 0.22);
    const topH = Math.round(H * 0.22);
    const centerH = Math.round(H * 0.36); // reserved; not directly used
    const bottomH = Math.round(H * 0.22);

    // Rectangles per sector (disjoint by construction)
    const centerRect = {
      x0: M + sideW + gW,
      x1: W - M - sideW - gW,
      y0: M + topH + gH,
      y1: H - M - bottomH - gH
    };
    const topRect = { x0: centerRect.x0, x1: centerRect.x1, y0: M, y1: M + topH };
    const bottomRect = { x0: centerRect.x0, x1: centerRect.x1, y0: H - M - bottomH, y1: H - M };
    const leftRect = { x0: M, x1: M + sideW, y0: centerRect.y0, y1: centerRect.y1 };
    const rightRect = { x0: W - M - sideW, x1: W - M, y0: centerRect.y0, y1: centerRect.y1 };

    // Split left lane vertically into Bedürfnis (upper) and Trigger (lower) proportional to counts
    const bed = cy.nodes().filter(n => toCanonicalType(n.data('type')) === 'bedürfnis').toArray();
    const trg = cy.nodes().filter(n => toCanonicalType(n.data('type')) === 'trigger').toArray();
    const ziel = cy.nodes().filter(n => toCanonicalType(n.data('type')) === 'schulentwicklungsziel').toArray();
    const pro = cy.nodes().filter(n => toCanonicalType(n.data('type')) === 'promotor').toArray();
    const bar = cy.nodes().filter(n => toCanonicalType(n.data('type')) === 'barriere').toArray();
    const out = cy.nodes().filter(n => toCanonicalType(n.data('type')) === 'outcome').toArray();

    const totalLeft = (bed.length + trg.length) || 1;
    const bedFrac = bed.length / totalLeft;
    const bedRect = { x0: leftRect.x0, x1: leftRect.x1, y0: leftRect.y0, y1: Math.round(leftRect.y0 + (leftRect.y1 - leftRect.y0) * bedFrac) };
    const trgRect = { x0: leftRect.x0, x1: leftRect.x1, y0: bedRect.y1, y1: leftRect.y1 };

    cy.startBatch();
    const minGap = Math.max(12, Math.round(20 * spacing));
    packIntoRect(ziel, centerRect, minGap);
    packIntoRect(pro, topRect, minGap);
    packIntoRect(bar, bottomRect, minGap);
    packIntoRect(bed, bedRect, minGap);
    packIntoRect(trg, trgRect, minGap);
    packIntoRect(out, rightRect, minGap);
    cy.endBatch();

    cy.animate({ center: { eles: cy.nodes() } }, { duration: 120 });
  }

  // Lay out only the nodes of the currently selected type into its sector
  function layoutSingleBucket(filterType, spacing = 1.0) {
    if (!cy) return;
    const cont = document.getElementById('cy');
    const W = Math.max(800, (cont?.clientWidth || 1000));
    const H = Math.max(600, (cont?.clientHeight || 700));

    const M = 24;
    const gW = Math.max(24, Math.round(W * 0.02));
    const gH = Math.max(24, Math.round(H * 0.02));
    const sideW = Math.round(W * 0.22);
    const topH = Math.round(H * 0.22);
    const bottomH = Math.round(H * 0.22);

    const centerRect = { x0: M + sideW + gW, x1: W - M - sideW - gW, y0: M + topH + gH, y1: H - M - bottomH - gH };
    const topRect    = { x0: centerRect.x0, x1: centerRect.x1, y0: M, y1: M + topH };
    const bottomRect = { x0: centerRect.x0, x1: centerRect.x1, y0: H - M - bottomH, y1: H - M };
    const leftRect   = { x0: M, x1: M + sideW, y0: centerRect.y0, y1: centerRect.y1 };
    const rightRect  = { x0: W - M - sideW, x1: W - M, y0: centerRect.y0, y1: centerRect.y1 };

    const minGap = Math.max(12, Math.round(20 * spacing));
    let bucketNodes, rect;
    switch (filterType) {
      case 'schulentwicklungsziel':
        bucketNodes = cy.nodes().filter(n => toCanonicalType(n.data('type')) === 'schulentwicklungsziel').toArray();
        rect = centerRect; break;
      case 'bedürfnis':
        bucketNodes = cy.nodes().filter(n => toCanonicalType(n.data('type')) === 'bedürfnis').toArray();
        rect = { x0: leftRect.x0, x1: leftRect.x1, y0: leftRect.y0, y1: Math.round((leftRect.y0 + leftRect.y1)/2) };
        break;
      case 'trigger':
        bucketNodes = cy.nodes().filter(n => toCanonicalType(n.data('type')) === 'trigger').toArray();
        rect = { x0: leftRect.x0, x1: leftRect.x1, y0: Math.round((leftRect.y0 + leftRect.y1)/2), y1: leftRect.y1 };
        break;
      case 'promotor':
        bucketNodes = cy.nodes().filter(n => toCanonicalType(n.data('type')) === 'promotor').toArray();
        rect = topRect; break;
      case 'barriere':
        bucketNodes = cy.nodes().filter(n => toCanonicalType(n.data('type')) === 'barriere').toArray();
        rect = bottomRect; break;
      case 'outcome':
        bucketNodes = cy.nodes().filter(n => toCanonicalType(n.data('type')) === 'outcome').toArray();
        rect = rightRect; break;
      default:
        bucketNodes = cy.nodes().toArray();
        rect = centerRect; break;
    }

    cy.startBatch();
    packIntoRect(bucketNodes, rect, minGap);
    cy.endBatch();
    cy.animate({ center: { eles: bucketNodes } }, { duration: 120 });
  }

  // Center button
  const centerBtn = document.getElementById("centerViewBtn");
  centerBtn.addEventListener("click", () => { if (cy) cy.fit(); });

  addNodeBtn?.addEventListener('click', () => {
    addNodeMode = !addNodeMode;
    addNodeBtn.setAttribute('data-active', addNodeMode ? 'true' : 'false');
  });

  deleteSelectedBtn?.addEventListener('click', async () => {
    if (!cy) return;
    const personId = personInput.value.trim();
    const nodes = cy.$('node:selected');
    for (let i = 0; i < nodes.length; i++) {
      const n = nodes[i];
      try {
        const res = await fetch(`${API_BASE_URL}/struktur/${personId}/${n.id()}`, { method: 'DELETE' });
        if (!res.ok) throw new Error(await res.text());
        n.remove();
        // Sync in-memory dataset
        const removedId = n.id();
        lastNodes = lastNodes.filter(nn => nn.id !== removedId);
        lastEdges = lastEdges.filter(ee => ee.from !== removedId && ee.to !== removedId);
      } catch (e) {
        alert(`Delete failed for ${n.id()}: ${e.message}`);
      }
    }
    reRender();
  });

  layoutSelect?.addEventListener('change', () => applyLayout(layoutSelect.value));
  saveLayoutBtn?.addEventListener('click', () => persistPositions());

  // Filter change listener
  filterTypeSelect?.addEventListener('change', () => {
    currentFilter = filterTypeSelect.value || 'all';
    // Keep expansion as-is; we don’t remove nodes anymore (we ghost/style them)
    needsLayout = true; // re-space deterministically for the new subset
    reRender();
  });

  // Helper: combined filter + focus (nothing disappears)
  function applyFilterAndFocusClasses() {
    if (!cy) return;
    const f = toCanonicalType(currentFilter || 'all');

    // clear old classes
    cy.nodes().removeClass('ghost primary neighbor faded highlight');
    cy.edges().removeClass('ghost-edge hide-edge');

    // --- Filter layer ---
    if (f === 'all') {
      cy.nodes().addClass('primary'); // everyone is primary
    } else {
      const primarySet = new Set();
      cy.nodes().forEach(n => {
        if (toCanonicalType(n.data('type')) === f) {
          n.addClass('primary');
          primarySet.add(n.id());
        } else {
          n.addClass('ghost'); // faint but visible
        }
      });
      cy.edges().forEach(e => {
        const s = e.data('source'), t = e.data('target');
        const sP = primarySet.has(s), tP = primarySet.has(t);
        if (sP && tP) {
          // keep normal edge
        } else if (sP || tP) {
          e.addClass('ghost-edge'); // show “would-be” connections as dashed/translucent
        } else {
          e.addClass('hide-edge');  // ghost→ghost edges disappear
        }
      });
    }

    // --- Focus layer (expansion) ---
    if (expandedNodeId) {
      const center = cy.$id(expandedNodeId);
      if (center && !center.empty()) {
        const hood = center.closedNeighborhood();
        cy.elements().addClass('faded');        // dim everything
        hood.removeClass('faded');              // undim the focused hood
        hood.nodes().removeClass('ghost')       // ensure fully visible in hood even if not the filter type
                   .addClass('highlight');
        hood.edges().removeClass('ghost-edge hide-edge');
        center.addClass('highlight');
      }
    }
  }

  connectSelectedBtn?.addEventListener('click', () => {
    if (!cy) return;
    const sel = cy.$('node:selected');
    if (sel.length !== 2) { alert('Select exactly two nodes'); return; }
    const a = sel[0].id(); const b = sel[1].id();
    const personId = personInput.value.trim();
    const newEdge = { from: a, to: b, label: '' };

    // Persist
    fetch(`${API_BASE_URL}/submit`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ personId, nodes: [], edges: [newEdge] })
    }).catch(err => console.error('Persist edge failed', err));

    // Update in-memory + re-render
    const exists = lastEdges.some(e => e.from === a && e.to === b);
    if (!exists) lastEdges.push({ ...newEdge, type: '', detail: '' });
    reRender();
  });

  function openInspector(el) {
    inspectorSelection = el;
    if (!el) { closeInspector(); return; }
    const isNode = el.isNode();
    selTypeSpan.textContent = isNode ? `Node ${el.id()}` : `Edge ${el.id()}`;
    const d = el.data();
    fieldLabel.value = d.label || '';
    fieldType.value = d.type || '';
    fieldTime.value = (d.time && /^\d{4}-\d{2}-\d{2}$/.test(d.time)) ? d.time : '';
    fieldColor.value = d.color || '';
    fieldDetail.value = d.detail || '';
    inspector.classList.remove('hidden');
    document.getElementById('cy')?.classList.add('has-panel');
  }
  function closeInspector() {
    inspectorSelection = null;
    inspector.classList.add('hidden');
    document.getElementById('cy')?.classList.remove('has-panel');
  }
  async function saveInspector() {
    if (!inspectorSelection) return;
    const isNode = inspectorSelection.isNode();
    const personId = personInput.value.trim();
    if (!personId) { alert('Set Person ID first'); return; }
    const pos = isNode ? inspectorSelection.position() : null;
    const payload = {
      personId,
      nodes: isNode ? [{
        id: inspectorSelection.id(),
        label: fieldLabel.value.trim(),
        type: fieldType.value,
        time: fieldTime.value || '',
        color: fieldColor.value.trim(),
        detail: fieldDetail.value.trim(),
        x: Math.round(pos.x), y: Math.round(pos.y),
        personId, isNode: true
      }] : [],
      edges: (!isNode) ? [{
        from: inspectorSelection.data('source'),
        to: inspectorSelection.data('target'),
        label: fieldLabel.value.trim(),
        type: fieldType.value,
        detail: fieldDetail.value.trim()
      }] : []
    };
    // Sync in-memory dataset
    if (isNode) {
      const idx = lastNodes.findIndex(n => n.id === inspectorSelection.id());
      const posxy = pos ? { x: Math.round(pos.x), y: Math.round(pos.y) } : {};
      const updated = {
        id: inspectorSelection.id(),
        label: fieldLabel.value.trim(),
        type: fieldType.value,
        time: fieldTime.value || '',
        color: fieldColor.value.trim(),
        detail: fieldDetail.value.trim(),
        personId,
        ...posxy
      };
      if (idx >= 0) { lastNodes[idx] = { ...lastNodes[idx], ...updated }; }
      else { lastNodes.push(updated); }
    } else {
      const from = inspectorSelection.data('source');
      const to = inspectorSelection.data('target');
      const idx = lastEdges.findIndex(e => e.from === from && e.to === to);
      const updated = {
        from, to,
        label: fieldLabel.value.trim(),
        type: fieldType.value,
        detail: fieldDetail.value.trim()
      };
      if (idx >= 0) { lastEdges[idx] = { ...lastEdges[idx], ...updated }; }
      else { lastEdges.push(updated); }
    }
    // Update UI immediately
    inspectorSelection.data({
      label: fieldLabel.value.trim(),
      type: fieldType.value,
      time: fieldTime.value || '',
      color: fieldColor.value.trim(),
      detail: fieldDetail.value.trim()
    });
    try {
      await fetch(`${API_BASE_URL}/submit`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) });
    } catch (e) { console.error('Save inspector failed', e); }
    reRender();
  }
  function persistPositions() {
    if (!cy) return;
    const personId = personInput.value.trim();
    const nodes = cy.nodes().map(n => {
      const p = n.position();
      return {
        id: n.id(),
        label: n.data('label')||'',
        type: n.data('type')||'',
        time: n.data('time')||'',
        color: n.data('color')||'',
        detail: n.data('detail')||'',
        x: Math.round(p.x), y: Math.round(p.y),
        personId, isNode: true
      };
    });
    // Keep in-memory positions in sync
    nodes.forEach(ns => {
      const i = lastNodes.findIndex(n => n.id === ns.id);
      if (i >= 0) lastNodes[i] = { ...lastNodes[i], ...ns };
    });
    fetch(`${API_BASE_URL}/submit`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ personId, nodes, edges: [] })
    });
  }
  function applyLayout(kind) {
    if (!cy) return;
    if (kind === 'cose') {
      cy.layout(getCoseOptions()).run();
    } else if (kind === 'grid') {
      cy.layout({ name: 'grid', rows: undefined }).run();
    } else if (kind === 'timeline') {
      const nodes = cy.nodes();
      const times = nodes.map(n => Date.parse(n.data('time')||'')).filter(v => !Number.isNaN(v));
      if (times.length === 0) return;
      const min = Math.min(...times), max = Math.max(...times);
      const span = Math.max(1, max - min);
      const width = document.getElementById('cy').clientWidth - 360; // some right margin for panel
      const x0 = 40, x1 = Math.max(200, width - 40);
      const typeY = (t) => {
        switch (toCanonicalType(t)) {
          case 'bedürfnis': return 150;
          case 'trigger': return 300;
          case 'schulentwicklungsziel': return 450;
          case 'barriere': return 600;
          case 'promotor': return 750;
          case 'outcome': return 900;
          default: return 1050;
        }
      };
      nodes.forEach(n => {
        const t = Date.parse(n.data('time')||'');
        if (!Number.isNaN(t)) {
          const ratio = (t - min) / span;
          const x = Math.round(x0 + ratio * (x1 - x0));
          n.position({ x, y: typeY(n.data('type')) });
        }
      });
      cy.fit();
    } else if (kind === 'compass') {
      const t = Math.max(0, Math.min(1, layoutStrength/100));
      const spacing = 0.8 + t * 0.6;
      layoutCompass(spacing);
    } else {
      // 'free' - do nothing
    }
  }

  saveInspectorBtn?.addEventListener('click', saveInspector);
  cancelInspectorBtn?.addEventListener('click', closeInspector);

  async function loadPersonData(personId) {
    try {
      const response = await fetch(`${API_BASE_URL}/struktur/${personId}`);
      if (!response.ok) {
        const errorText = await response.text();
        throw new Error(`Server error ${response.status}: ${errorText}`);
      }
      const data = await response.json();
      // Store canonical dataset
      lastNodes = Array.isArray(data.nodes) ? data.nodes.slice() : [];
      lastEdges = Array.isArray(data.edges) ? data.edges.slice() : [];
      const status = document.getElementById("status");
      if (DEBUG_STATUS && status) {
        status.textContent = JSON.stringify({ nodes: lastNodes, edges: lastEdges }, null, 2);
      }
      // Default layout: single-type Schulentwicklungsziele neatly placed
      needsLayout = true;
      reRender();
    } catch (err) {
      console.error("Failed to load strukturbild:", err);
      const status = document.getElementById("status");
      if (DEBUG_STATUS && status) {
        status.textContent = `Error loading strukturbild: ${err.message}`;
      }
    }
  }

  // ===== Render & layout orchestration =====
  function reRender() {
    if (reRenderPending) return;
    reRenderPending = true;
    const cont = document.getElementById("cy");
    const prevZoom = cy ? cy.zoom() : null;
    const prevPan = cy ? cy.pan() : null;
    const prevOverflowBody = document.body.style.overflow;
    const prevOverflowCont = cont ? cont.style.overflow : null;
    if (cont) cont.style.overflow = 'hidden';
    document.body.style.overflow = 'hidden';

    requestAnimationFrame(() => {
      try {
        // Always show full dataset; filtering/expansion is done via classes (ghost/faded/etc.)
        const nodes = lastNodes, edges = lastEdges;
        const status = document.getElementById("status");
        if (DEBUG_STATUS && status) status.textContent = JSON.stringify({ nodes, edges }, null, 2);
        renderCytoscape(nodes, edges);

        // Layout strategy:
        // - All types: deterministic compass (stable sectors)
        // - Single-type filters: deterministic lane/grid for that type
        // Note: Expansion does NOT change layout; we only dim non-neighborhood.
        if (cy) {
          const t = Math.max(0, Math.min(1, layoutStrength/100));
          const spacing = 0.8 + t * 0.6; // 0.8..1.4
          if (currentFilter === 'all') {
            layoutCompass(spacing);
          } else {
            layoutSingleBucket(toCanonicalType(currentFilter), spacing);
          }
          needsLayout = false;
        }

        // Combined filter + focus styling
        applyFilterAndFocusClasses();

        if (cy && prevZoom != null && prevPan != null) {
          // Keep user viewport (we already recentered briefly in layout)
          cy.zoom(prevZoom);
          cy.pan(prevPan);
        }
      } finally {
        if (cont && prevOverflowCont !== null) cont.style.overflow = prevOverflowCont;
        document.body.style.overflow = prevOverflowBody;
        reRenderPending = false;
      }
    });
  }

  function renderCytoscape(nodes, edges) {
    if (!cy) {
      cy = cytoscape({
        container: document.getElementById("cy"),
        style: [
          {
            selector: 'node[color = ""], node[!color]',
            style: { 'background-color': '#666' }
          },
          // rely on data(color) for all types
          {
            selector: 'node',
            style: {
              'shape': 'round-rectangle',
              'label': 'data(label)',
              'text-wrap': 'wrap',
              'text-max-width': 200,
              'text-valign': 'center',
              'text-halign': 'center',
              'text-margin-y': 0,
              'font-size': 12,
              'color': '#fff',
              'background-color': 'data(color)',
              'background-opacity': 1,
              'width': 220,
              'height': 90,
              'border-width': 2,
              'border-color': '#000',
              'overlay-padding': '6px',
              'overlay-color': '#000',
              'overlay-opacity': 0
            }
          },
          {
            selector: 'node:selected',
            style: { 'border-width': 3, 'border-color': '#FFD700' }
          },
          {
            selector: '.faded',
            style: {
              'opacity': 0.15,
              'text-opacity': 0.2,
              'events': 'no'
            }
          },
          {
            selector: '.highlight',
            style: {
              'border-width': 4,
              'border-color': '#FFD700'
            }
          },
          {
            selector: '.ghost',
            style: {
              'opacity': 0.15,
              'text-opacity': 0.2,
              'background-opacity': 0.18,
              'border-opacity': 0.25
            }
          },
          {
            selector: 'edge.ghost-edge',
            style: {
              'opacity': 0.25,
              'line-style': 'dashed',
              'line-dash-pattern': [6, 4]
            }
          },
          {
            selector: 'edge.hide-edge',
            style: {
              'opacity': 0,
              'events': 'no'
            }
          },
          {
            selector: 'edge',
            style: {
              'width': 2,
              'line-color': '#ccc',
              'target-arrow-color': '#ccc',
              'target-arrow-shape': 'triangle',
              'curve-style': 'bezier',
              'label': 'data(label)',
              'font-size': 10,
              'text-rotation': 'autorotate',
              'text-margin-y': -10,
              'color': '#555',
              'text-background-color': '#fff',
              'text-background-opacity': 1,
              'text-background-padding': 2
            }
          }
        ],
        elements: cyElements(nodes, edges)
      });

      // Cache initial container size to avoid first-run resize oscillations
      const cont = document.getElementById('cy');
      if (cont) {
        lastContainerW = cont.clientWidth || 0;
        lastContainerH = cont.clientHeight || 0;
      }

      // Context menu (delete)
      const defaults = {
        menuItems: [
          {
            id: 'delete',
            content: 'Delete',
            selector: 'node',
            onClickFunction: (event) => {
              const node = event.target;
              const personId = document.getElementById("personIdInput").value;
              fetch(`${API_BASE_URL}/struktur/${personId}/${node.id()}`, { method: 'DELETE' })
                .then(res => {
                  if (!res.ok) return res.text().then(t => { throw new Error(`Delete failed ${res.status}: ${t}`); });
                  node.remove();
                })
                .catch(err => {
                  console.error('Delete error', err);
                  alert(`Delete failed: ${err.message}`);
                });
            }
          }
        ],
        menuItemClasses: ['custom-menu-item'],
        contextMenuClasses: ['custom-menu']
      };
      try { cy.contextMenus(defaults); } catch {}

      // Double-tap expansion: two taps on the SAME node within DOUBLE_TAP_MS
      cy.on('tap', 'node', (evt) => {
        if (addNodeMode) return;
        const id = evt.target.id();
        const now = Date.now();
        const isSame = (lastTapNodeId === id);
        const isDouble = isSame && (now - lastTapTime) <= DOUBLE_TAP_MS;
        lastTapNodeId = id;
        lastTapTime = now;
        if (!isDouble) return; // single tap just selects

        const wasExpanded = !!expandedNodeId;
        const collapsingSame = wasExpanded && expandedNodeId === id;
        if (collapsingSame) {
          // collapse: restore previous filter if any
          expandedNodeId = null;
          if (prevFilterBeforeExpand !== null) {
            currentFilter = prevFilterBeforeExpand;
            if (filterTypeSelect) filterTypeSelect.value = prevFilterBeforeExpand;
            prevFilterBeforeExpand = null;
          }
        } else {
          // expand: show the whole picture in Compass, don't hide anything
          prevFilterBeforeExpand = currentFilter;
          expandedNodeId = id;
          currentFilter = 'all';
          if (filterTypeSelect) filterTypeSelect.value = 'all';
          if (layoutSelect) layoutSelect.value = 'compass';
          needsLayout = true;
        }
        reRender();

        if (expandedNodeId) {
          const tgt = cy.$id(expandedNodeId);
          if (tgt && !tgt.empty()) {
            cy.animate({ center: { eles: tgt } }, { duration: 200 });
          }
        }
      });

      // Optional "Clear Expand"
      clearExpandBtn?.addEventListener('click', () => {
        expandedNodeId = null;
        if (prevFilterBeforeExpand !== null) {
          currentFilter = prevFilterBeforeExpand;
          if (filterTypeSelect) filterTypeSelect.value = prevFilterBeforeExpand;
          prevFilterBeforeExpand = null;
        }
        needsLayout = true;
        reRender();
      });

      cy.on('select', 'node,edge', (evt) => { openInspector(evt.target); });
      cy.on('unselect', 'node,edge', () => {
        if (cy.$('node:selected,edge:selected').length === 0) closeInspector();
      });

      // Add node mode: click on background to place new node
      cy.on('tap', (evt) => {
        if (evt.target === cy) {
          if (addNodeMode) {
            const p = evt.position;
            const id = (typeof crypto !== 'undefined' && crypto.randomUUID) ? crypto.randomUUID() : (Math.random()*1e17).toString(36);
            const personId = personInput.value.trim();
            cy.add({ group:'nodes', data:{ id, label:'', type:'', time:'', color:'', detail:'' }, position:{ x:p.x, y:p.y } });
            // Update in-memory dataset
            const nodeObj = { id, label:'', type:'', time:'', color:'', detail:'', x:Math.round(p.x), y:Math.round(p.y), personId, isNode:true };
            lastNodes.push(nodeObj);
            // Persist basic node immediately
            fetch(`${API_BASE_URL}/submit`, {
              method:'POST', headers:{'Content-Type':'application/json'},
              body: JSON.stringify({ personId, nodes:[nodeObj], edges:[] })
            });
            const newNode = cy.$id(id);
            newNode.select();
            openInspector(newNode);
            addNodeMode = false; addNodeBtn?.setAttribute('data-active','false');
          }
        }
      });

    } else {
      // Update existing cy
      cy.startBatch();
      cy.elements().remove();
      cy.add(cyElements(nodes, edges));
      cy.endBatch();
      cy.style().update();

      const cont = document.getElementById('cy');
      if (cont) {
        const w = cont.clientWidth || 0;
        const h = cont.clientHeight || 0;
        if (Math.abs(w - lastContainerW) > 1 || Math.abs(h - lastContainerH) > 1) {
          cy.resize();
          lastContainerW = w;
          lastContainerH = h;
        }
      }

      // Only auto-layout when we truly have no positions
      const hasPositions = (nodes || []).some(n => typeof n.x === 'number' && typeof n.y === 'number');
      if (!hasPositions) {
        cy.layout({ name: 'cose' }).run();
      }
    }
  }

  function cyElements(nodes, edges) {
    const cyNodes = (nodes || []).map(n => {
      const canon = toCanonicalType(n.type);
      const color = (n.color && n.color.trim()) ? n.color : (TYPE_COLORS[canon] || '#666');
      return {
        data: { id: n.id, label: n.label, type: n.type || '', time: n.time || '', color, detail: n.detail || '' },
        position: { x: n.x || 0, y: n.y || 0 }
      };
    });

    const cyEdges = (edges || [])
      .filter(e => e.from && e.to)
      .map(e => ({
        data: {
          id: `${e.from}-${e.to}`,
          source: e.from,
          target: e.to,
          label: e.label || '',
          type: e.type || '',
          detail: e.detail || ''
        }
      }));

    return [...cyNodes, ...cyEdges];
  }

  // Wire buttons
  loadBtn.addEventListener("click", async (e) => {
    e.preventDefault();
    const personId = personInput.value.trim();
    if (!personId) { alert("Please provide a Person ID"); return; }
    await loadPersonData(personId);
    currentFilter = 'schulentwicklungsziel';
    expandedNodeId = null;
    prevFilterBeforeExpand = null;
    if (filterTypeSelect) filterTypeSelect.value = 'schulentwicklungsziel';
    needsLayout = true;
    reRender();
  });

  createBtn.addEventListener("click", (e) => {
    e.preventDefault();
    const personId = personInput.value;
    if (!personId) { alert("Please provide a Person ID first."); return; }
    if (DEBUG_STATUS) {
      document.getElementById("status").textContent = `New person '${personId}' created.`;
    }
    renderCytoscape([], []);
    lastNodes = [];
    lastEdges = [];
    currentFilter = 'schulentwicklungsziel';
    expandedNodeId = null;
    prevFilterBeforeExpand = null;
    if (filterTypeSelect) filterTypeSelect.value = 'schulentwicklungsziel';
    needsLayout = true;
    reRender();
  });
});