const API_BASE_URL = window.STRUKTURBILD_API_URL;

function decodeURIComponentSafe(value) {
  if (typeof value !== "string" || value.length === 0) return "";
  try {
    return decodeURIComponent(value);
  } catch (err) {
    return value;
  }
}

function extractStoryIdFromLocation(loc = window.location) {
  if (!loc) return "";
  const pathname = loc.pathname || "";
  const pathMatch = pathname.match(/\/stories\/([^/]+)/);
  if (pathMatch && pathMatch[1]) {
    return decodeURIComponentSafe(pathMatch[1]);
  }
  try {
    const params = new URLSearchParams(loc.search || "");
    const queryStory = params.get("storyId") || params.get("storyID");
    if (queryStory) return queryStory;
  } catch (err) {
    // Ignore environments without URLSearchParams support
  }
  if (loc.hash) {
    const hashMatch = loc.hash.match(/story(?:Id)?=([^&]+)/i);
    if (hashMatch && hashMatch[1]) {
      return decodeURIComponentSafe(hashMatch[1]);
    }
  }
  return "";
}

function getBasePath() {
  if (typeof window === "undefined" || !window.location) return "/";
  const { location } = window;
  const fallbackPath = (location.pathname || "/").replace(/\/stories\/[^/]*$/, "/");
  try {
    const url = new URL(location.href);
    url.search = "";
    url.hash = "";
    url.pathname = url.pathname.replace(/\/stories\/[^/]*$/, "/");
    url.pathname = url.pathname.replace(/\/[^/]*\.html?$/i, "/");
    let path = url.pathname || "/";
    if (!path.endsWith("/")) path += "/";
    if (!path.startsWith("/")) path = `/${path}`;
    return path === "//" ? "/" : path;
  } catch (err) {
    let path = fallbackPath.replace(/\/[^/]*\.html?$/i, "/");
    if (!path.endsWith("/")) path += "/";
    if (!path.startsWith("/")) path = `/${path}`;
    return path === "//" ? "/" : path;
  }
}

let STORY_ID = extractStoryIdFromLocation();
let IS_STORY_MODE = !!STORY_ID;

async function fetchAndRenderStory(storyId) {
  const root = document.getElementById('story-root');
  if (!root || !storyId) return;
  const base = (API_BASE_URL || '').replace(/\/+$/, '');
  try {
    const res = await fetch(`${base}/api/stories/${encodeURIComponent(storyId)}/full`);
    if (!res.ok) {
      root.innerHTML = `<p style="color:#b00">Story load failed (HTTP ${res.status}).</p>`;
      return;
    }
    const data = await res.json();
    const title = (data.story && data.story.title) || storyId;
    const paras = Array.isArray(data.paragraphs)
      ? data.paragraphs.slice().sort((a, b) => (a.index || 0) - (b.index || 0))
      : [];
    let html = `<h2>${title}</h2>`;
    if (!paras.length) html += '<p style="color:#555">No paragraphs yet.</p>';
    else {
      html += paras.map(p => {
        const body = (p.bodyMd || '').replace(/\n/g, '<br>');
        const t = (p.index != null ? `§${p.index}` : '');
        const heading = p.title ? `: ${p.title}` : '';
        return `<div class="para"><div class="para-index">${t}${heading}</div><div class="para-body">${body}</div></div>`;
      }).join('');
    }
    root.innerHTML = html;
  } catch (e) {
    console.error('Story fetch error', e);
    root.innerHTML = `<p style="color:#b00">Story load error: ${String(e)}</p>`;
  }
}

async function fetchStoryList() {
  const base = (API_BASE_URL || '').replace(/\/+$/, '');
  const res = await fetch(`${base}/api/stories`);
  if (!res.ok) {
    throw new Error(`HTTP ${res.status}`);
  }
  const data = await res.json();
  const storiesArray = data && Array.isArray(data.stories) ? data.stories : null;
  const collection = storiesArray || (Array.isArray(data) ? data : []);
  return collection
    .filter(item => item && (item.storyId || item.storyID))
    .map(item => ({
      storyId: item.storyId || item.storyID,
      title: item.title || (item.Story && item.Story.title) || item.storyId || item.storyID,
      schoolId: item.schoolId || item.SchoolID || '',
    }));
}

document.addEventListener("DOMContentLoaded", () => {
  // --- Ensure the inline node/edge editor exists (bottom-left) ---
  (function ensureInspectorUI() {
    // Inject minimal styles once
    if (!document.getElementById('inspector-inline-style')) {
      const style = document.createElement('style');
      style.id = 'inspector-inline-style';
      style.textContent = `
        #inspector { position: fixed; left: 12px; bottom: 12px; width: 340px; max-height: 44vh;
          overflow: auto; background: rgba(255,255,255,0.98); border: 1px solid rgba(0,0,0,0.1);
          border-radius: 8px; box-shadow: 0 6px 18px rgba(0,0,0,0.15); padding: 10px; z-index: 2000; }
        #inspector.hidden { display: none; }
        #inspector h4 { margin: 0 0 8px 0; font-size: 13px; color: #111; }
        #inspector small { color: #555; }
        #inspector form { display: grid; grid-template-columns: 1fr; gap: 8px; }
        #inspector label { font-size: 12px; color: #444; }
        #inspector input[type="text"],
        #inspector input[type="date"],
        #inspector textarea,
        #inspector select { width: 100%; font-size: 12px; padding: 6px 8px; border-radius: 6px;
          border: 1px solid rgba(0,0,0,0.2); box-sizing: border-box; }
        #inspector textarea { min-height: 70px; resize: vertical; }
        #inspector .row { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
        #inspector .actions { display: flex; gap: 8px; justify-content: flex-end; }
        #inspector button { font-size: 12px; padding: 6px 10px; border-radius: 6px; border: 1px solid rgba(0,0,0,0.2); cursor: pointer; }
        #inspector button.primary { background: #2563eb; color: #fff; border-color: #1d4ed8; }
        #inspector button.ghosty { background: #f8f9fb; }
      `;
      document.head.appendChild(style);
    }

    // Inject panel markup if missing
    if (!document.getElementById('inspector')) {
      const panel = document.createElement('div');
      panel.id = 'inspector';
      panel.className = 'hidden';
      panel.innerHTML = `
        <h4>Editor <small id="inspectorSelectedType"></small></h4>
        <form id="inspectorForm" autocomplete="off">
          <div>
            <label for="fieldLabel">Label</label>
            <input id="fieldLabel" type="text" placeholder="Label">
          </div>
          <div class="row">
            <div>
              <label for="fieldType">Type</label>
              <select id="fieldType">
                <option value=""></option>
                <option value="prozess">prozess</option>
                <option value="praxis">praxis</option>
                <option value="ergebnis">ergebnis</option>
                <option value="schwierigkeit">schwierigkeit</option>
                <option value="beschäftigung">beschäftigung</option>
              </select>
            </div>
            <div>
              <label for="fieldTime">Date</label>
              <input id="fieldTime" type="date" />
            </div>
          </div>
          <div class="row">
            <div>
              <label for="fieldColor">Color</label>
              <input id="fieldColor" type="text" placeholder="#hex or name">
            </div>
            <div>
              <label for="fieldDetail">Detail</label>
              <input id="fieldDetail" type="text" placeholder="Short note">
            </div>
          </div>
          <div class="actions">
            <button id="cancelInspectorBtn" type="button" class="ghosty">Close</button>
            <button id="saveInspectorBtn" type="button" class="primary">Save</button>
          </div>
        </form>
      `;
      document.body.appendChild(panel);

      // Optional: Cmd/Ctrl+Enter to save
      const form = panel.querySelector('#inspectorForm');
      form.addEventListener('keydown', (ev) => {
        if ((ev.ctrlKey || ev.metaKey) && ev.key === 'Enter') {
          ev.preventDefault();
          try { saveInspector(); } catch (e) {}
        }
      });
    }
  })();
  // --- end ensure inspector ---
  const storyRoot = document.getElementById('story-root');
  const placeholderHtml = '<p style="color:#444">Wähle eine Story in der Liste, um sie zu öffnen.</p>';

  const loadBtn = document.getElementById("loadPersonBtn") || document.getElementById("loadBtn");
  const createBtn = document.getElementById("createPersonBtn") || document.getElementById("createBtn");
  const storyInput = document.getElementById("storyIdInput");
  const storySelect = document.getElementById("storySelect");
  const filterTypeSelect = document.getElementById("filterTypeSelect");

  const applyPlaceholder = () => {
    if (storyRoot) {
      storyRoot.innerHTML = placeholderHtml;
    }
  };

  const ensureStoryRendered = (storyId, { skipUrlUpdate = false, forceReload = false } = {}) => {
    if (!storyId) {
      STORY_ID = "";
      IS_STORY_MODE = false;
      document.body.classList.remove("story-mode");
      if (storyInput) storyInput.value = "";
      if (storySelect) {
        storySelect.value = "";
      }
      currentFilter = 'schulentwicklungsziel';
      if (filterTypeSelect) filterTypeSelect.value = 'schulentwicklungsziel';
      if (!skipUrlUpdate) {
        const basePath = getBasePath();
        if (window.history && window.history.pushState) {
          window.history.pushState({}, "", basePath);
        } else {
          window.location.assign(basePath);
          return;
        }
      }
      if (window.renderStoryPage) {
        window.renderStoryPage("");
      }
      applyPlaceholder();
      return;
    }

    if (!forceReload && STORY_ID === storyId && IS_STORY_MODE) {
      return;
    }

    STORY_ID = storyId;
    IS_STORY_MODE = true;

    if (!skipUrlUpdate) {
      const encoded = encodeURIComponent(storyId);
      const basePath = getBasePath();
      if (window.history && window.history.pushState) {
        const nextUrl = `${basePath}?storyId=${encoded}`;
        window.history.pushState({ storyId }, "", nextUrl);
      } else {
        const params = new URLSearchParams(window.location.search || "");
        params.set("storyId", storyId);
        window.location.assign(`${basePath}?${params.toString()}`);
        return;
      }
    }

    document.body.classList.add("story-mode");
    if (storyInput) storyInput.value = storyId;
    if (storySelect) {
      storySelect.value = storyId;
    }

    if (window.renderStoryPage) {
      window.renderStoryPage(storyId);
    } else {
      fetchAndRenderStory(storyId);
    }

    currentFilter = 'all';
    if (filterTypeSelect) filterTypeSelect.value = 'all';
    expandedNodeId = null;
    prevFilterBeforeExpand = null;

    loadStoryData(storyId)
      .then(() => {
        needsLayout = true;
        reRender();
      })
      .catch(err => console.error('Auto story load failed', err));
  };

  if (STORY_ID) {
    ensureStoryRendered(STORY_ID, { skipUrlUpdate: true, forceReload: true });
  } else {
    applyPlaceholder();
  }

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
  let currentFilter = STORY_ID ? 'all' : 'prozess'; // default depends on mode
  let prevFilterBeforeExpand = null;           // remember filter while expanded
  let expandedNodeId = null;                   // node id if expanded (focus)

  // UX tuning
  const DEBUG_STATUS = false; // write #status only when true (reduces scroll flicker)

  // === MVP Taxonomy + colors ===
  const TYPE_COLORS = {
    // MVP taxonomy only
    'prozess': '#2563eb',
    'praxis': '#7c3aed',
    'ergebnis': '#16a34a',
    'schwierigkeit': '#dc2626',
    'beschäftigung': '#f59e0b'
  };
  function stripDiacritics(s){
    try { return s.normalize('NFD').replace(/\p{Diacritic}/gu, ''); } catch { return s || ''; }
  }
  function toCanonicalType(t){
    const raw = (t ?? '').toString().trim().toLowerCase();
    const ascii = stripDiacritics(raw);
    const ALIASES = {
      // MVP taxonomy aliases + diacritic/typing variants only
      'prozess': 'prozess',
      'praxis': 'praxis',
      'ergebnis': 'ergebnis',
      'schwierigkeit': 'schwierigkeit',
      'beschaeftigung': 'beschäftigung',
      'beschaftigung': 'beschäftigung',
      'beschäftigung': 'beschäftigung'
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
  const deleteSelectedBtn = document.getElementById("deleteSelectedBtn") || document.getElementById("deleteBtn");
  const layoutSelect = document.getElementById("layoutSelect");
  const saveLayoutBtn = document.getElementById("saveLayoutBtn");
  const connectSelectedBtn = document.getElementById("connectSelectedBtn") || document.getElementById("connectBtn");
  let inspectorSelection = null; // cy element currently edited
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

  // MVP: deterministic compass layout with disjoint rectangles per sector
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
    const bottomH = Math.round(H * 0.22);

    // Rectangles per sector (disjoint by construction)
    const centerRect = {
      x0: M + sideW + gW,
      x1: W - M - sideW - gW,
      y0: M + topH + gH,
      y1: H - M - bottomH - gH
    };
    const topRect    = { x0: centerRect.x0, x1: centerRect.x1, y0: M,                  y1: M + topH };
    const bottomRect = { x0: centerRect.x0, x1: centerRect.x1, y0: H - M - bottomH,    y1: H - M   };
    const leftRect   = { x0: M,               x1: M + sideW,     y0: centerRect.y0,      y1: centerRect.y1 };
    const rightRect  = { x0: W - M - sideW,   x1: W - M,         y0: centerRect.y0,      y1: centerRect.y1 };

    // MVP buckets
    const prozess       = cy.nodes().filter(n => toCanonicalType(n.data('type')) === 'prozess').toArray();
    const praxis        = cy.nodes().filter(n => toCanonicalType(n.data('type')) === 'praxis').toArray();
    const ergebnis      = cy.nodes().filter(n => toCanonicalType(n.data('type')) === 'ergebnis').toArray();
    const schwierigkeit = cy.nodes().filter(n => toCanonicalType(n.data('type')) === 'schwierigkeit').toArray();
    const beschaeft     = cy.nodes().filter(n => toCanonicalType(n.data('type')) === 'beschäftigung').toArray();

    cy.startBatch();
    const minGap = Math.max(12, Math.round(20 * spacing));
    packIntoRect(prozess,       centerRect, minGap);
    packIntoRect(praxis,        topRect,    minGap);
    packIntoRect(schwierigkeit, bottomRect, minGap);
    packIntoRect(beschaeft,     leftRect,   minGap);
    packIntoRect(ergebnis,      rightRect,  minGap);
    cy.endBatch();

    cy.animate({ center: { eles: cy.nodes() } }, { duration: 120 });
  }

  // Lay out only the nodes of the currently selected MVP type into its sector
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
      case 'prozess':
        bucketNodes = cy.nodes().filter(n => toCanonicalType(n.data('type')) === 'prozess').toArray();
        rect = { x0: M + sideW + gW, x1: W - M - sideW - gW, y0: M + topH + gH, y1: H - M - bottomH - gH };
        break;
      case 'praxis':
        bucketNodes = cy.nodes().filter(n => toCanonicalType(n.data('type')) === 'praxis').toArray();
        rect = { x0: M + sideW + gW, x1: W - M - sideW - gW, y0: M, y1: M + topH };
        break;
      case 'schwierigkeit':
        bucketNodes = cy.nodes().filter(n => toCanonicalType(n.data('type')) === 'schwierigkeit').toArray();
        rect = { x0: M + sideW + gW, x1: W - M - sideW - gW, y0: H - M - bottomH, y1: H - M };
        break;
      case 'beschäftigung':
        bucketNodes = cy.nodes().filter(n => toCanonicalType(n.data('type')) === 'beschäftigung').toArray();
        rect = { x0: M, x1: M + sideW, y0: M + topH + gH, y1: H - M - bottomH - gH };
        break;
      case 'ergebnis':
        bucketNodes = cy.nodes().filter(n => toCanonicalType(n.data('type')) === 'ergebnis').toArray();
        rect = { x0: W - M - sideW, x1: W - M, y0: M + topH + gH, y1: H - M - bottomH - gH };
        break;
      default:
        bucketNodes = cy.nodes().toArray();
        rect = { x0: M + sideW + gW, x1: W - M - sideW - gW, y0: M + topH + gH, y1: H - M - bottomH - gH };
        break;
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
    const storyId = storyInput.value.trim();
    if (!storyId) { alert('Set Story ID first'); return; }
    const nodes = cy.$('node:selected');
    for (let i = 0; i < nodes.length; i++) {
      const n = nodes[i];
      try {
        const res = await fetch(`${API_BASE_URL}/struktur/${storyId}/${n.id()}`, { method: 'DELETE' });
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
    const storyId = storyInput.value.trim();
    if (!storyId) { alert('Set Story ID first'); return; }
    const newEdge = { from: a, to: b, label: '' };

    // Persist
    fetch(`${API_BASE_URL}/submit`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ storyId, nodes: [], edges: [newEdge] })
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
    if (selTypeSpan) selTypeSpan.textContent = isNode ? `Node ${el.id()}` : `Edge ${el.id()}`;
    const d = el.data();
    if (fieldLabel) fieldLabel.value = d.label || '';
    if (fieldType) fieldType.value = d.type || '';
    if (fieldTime) fieldTime.value = (d.time && /^\d{4}-\d{2}-\d{2}$/.test(d.time)) ? d.time : '';
    if (fieldColor) fieldColor.value = d.color || '';
    if (fieldDetail) fieldDetail.value = d.detail || '';
    if (inspector) {
      inspector.classList.remove('hidden');
      const cyEl = document.getElementById('cy');
      if (cyEl && cyEl.classList) cyEl.classList.add('has-panel');
    }
  }
  function closeInspector() {
    inspectorSelection = null;
    if (inspector) {
      inspector.classList.add('hidden');
    }
    const cyEl = document.getElementById('cy');
    if (cyEl && cyEl.classList) cyEl.classList.remove('has-panel');
  }
  async function saveInspector() {
    if (!inspectorSelection) return;
    const isNode = inspectorSelection.isNode();
    const storyId = storyInput.value.trim();
    if (!storyId) { alert('Set Story ID first'); return; }
    const pos = isNode ? inspectorSelection.position() : null;
    const payload = {
      storyId,
      nodes: isNode ? [{
        id: inspectorSelection.id(),
        label: fieldLabel.value.trim(),
        type: fieldType.value,
        time: fieldTime.value || '',
        color: fieldColor.value.trim(),
        detail: fieldDetail.value.trim(),
        x: Math.round(pos.x), y: Math.round(pos.y),
        storyId, isNode: true
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
        storyId,
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
    if (isNode) {
  inspectorSelection.data('hasXY', true);
}
    try {
      await fetch(`${API_BASE_URL}/submit`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) });
    } catch (e) { console.error('Save inspector failed', e); }
    reRender();
  }
  function persistPositions() {
    if (!cy) return;
    const storyId = storyInput.value.trim();
    if (!storyId) { alert('Set Story ID first'); return; }
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
        storyId, isNode: true
      };
    });
    // Keep in-memory positions in sync
    nodes.forEach(ns => {
      const i = lastNodes.findIndex(n => n.id === ns.id);
      if (i >= 0) lastNodes[i] = { ...lastNodes[i], ...ns };
    });
    // Mark saved XY locally so subsequent layouts don't move them
cy.nodes().forEach(n => n.data('hasXY', true));
    fetch(`${API_BASE_URL}/submit`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ storyId, nodes, edges: [] })
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
          case 'praxis': return 150;
          case 'beschäftigung': return 300;
          case 'prozess': return 450;
          case 'schwierigkeit': return 600;
          case 'ergebnis': return 750;
          default: return 900;
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

  async function loadStoryData(storyId) {
    if (!storyId) return;
    try {
      const response = await fetch(`${API_BASE_URL}/struktur/${storyId}`);
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

          // Determine saved positions from the source dataset (lastNodes), not from cy data
          const freeIdSet = new Set(
            (lastNodes || [])
              .filter(n => !(Number.isFinite(Number(n.x)) && Number.isFinite(Number(n.y))))
              .map(n => n.id)
          );
          const total = cy.nodes().length;

          if (freeIdSet.size === total || total === 0) {
            // No saved positions at all: use deterministic layouts
            if (currentFilter === 'all') {
              layoutCompass(spacing);
            } else {
              layoutSingleBucket(toCanonicalType(currentFilter), spacing);
            }
          } else if (freeIdSet.size > 0) {
            // Some nodes lack positions: lay out only those without saved XY
            const freeNodes = cy.nodes().filter(n => freeIdSet.has(n.id()));
            freeNodes.layout({ name: 'grid', rows: undefined }).run();
            cy.fit();
          } else {
            // All nodes have saved positions: do not move anything
            // Ensure positions are set from source once in case the add path was ignored
            cy.nodes().forEach(n => {
              const src = (lastNodes || []).find(nn => nn.id === n.id());
              if (src && Number.isFinite(Number(src.x)) && Number.isFinite(Number(src.y))) {
                n.position({ x: Number(src.x), y: Number(src.y) });
              }
            });
            cy.fit();
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

          // --- Paragraph focus styles (NEW) ---
          {
            selector: 'node.para-dim',
            style: { 'opacity': 0.4, 'text-opacity': 0.4 }
          },
          {
            selector: 'edge.para-dim',
            style: { 'opacity': 0.15, 'line-style': 'dashed', 'line-dash-pattern': [6, 4] }
          },
          {
            selector: 'node.para-focus',
            style: { 'border-width': 4, 'border-color': '#2563eb', 'opacity': 1, 'text-opacity': 1 }
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

      // === Inspector show/hide wiring ===
(function () {
  var inspector = document.getElementById('inspector');
  var cyContainer = document.getElementById('cy');
  var closeBtn = document.getElementById('inspector-close');

  function openInspector(node) {
    if (!inspector) return;
    inspector.classList.remove('hidden');
    if (cyContainer) cyContainer.classList.add('has-panel');

    // populate fields if present
    var idEl = document.getElementById('inspector-id');
    var labelEl = document.getElementById('inspector-label');
    var typeEl = document.getElementById('inspector-type');
    var colorEl = document.getElementById('inspector-color');

    if (node && node.id) {
      var data = node.data ? node.data() : {};
      if (idEl) idEl.value = data.id || '';
      if (labelEl) labelEl.value = data.label || '';
      if (typeEl) typeEl.value = data.type || '';
      if (colorEl) colorEl.value = data.color || '';
    }
  }

  function closeInspector() {
    if (!inspector) return;
    inspector.classList.add('hidden');
    if (cyContainer) cyContainer.classList.remove('has-panel');
  }

  // Show on node selection
  cy.on('select', 'node', function (evt) {
    openInspector(evt.target);
  });

  // Hide when no nodes remain selected
  cy.on('unselect', 'node', function () {
    if (cy.$('node:selected').length === 0) closeInspector();
  });

  // Click background closes inspector
  cy.on('tap', function (evt) {
    if (evt.target === cy) {
      cy.$(':selected').unselect();
      closeInspector();
    }
  });

  // Close button
  if (closeBtn) {
    closeBtn.addEventListener('click', function () {
      cy.$(':selected').unselect();
      closeInspector();
    });
  }
})();

      // --- Paragraph focus styling + handlers (MVP) ------------------------------
(function attachParagraphFocus() {
  if (!cy) return;

  // 1) Add styles once (Cytoscape has its own stylesheet; CSS files can't target it)
  if (!cy._paraStyleAdded) {
    cy.style()
      .selector('node.para-dim')
      .style({ opacity: 0.5, 'text-opacity': 0.5 })
      .selector('edge.para-dim')
      .style({ opacity: 0.2 })
      .selector('node.para-focus')
      .style({ 'border-width': 4, 'border-color': '#2563eb', opacity: 1, 'text-opacity': 1 })
      .update();
    cy._paraStyleAdded = true;
  }

  // 2) Helper to apply focus
function applyParagraphFocus(nodeIds) {
  if (!window.cy) return;
  const cyInstance = window.cy;
  cyInstance.elements().style('opacity', 1);   // reset every time
  if (!Array.isArray(nodeIds) || nodeIds.length === 0) return;
  const focusSet = new Set(nodeIds);
  cyInstance.elements().style('opacity', 0.5);
  cyInstance.nodes().filter(n => focusSet.has(n.id())).style('opacity', 1);
  cyInstance.edges().filter(e => focusSet.has(e.data('source')) || focusSet.has(e.data('target'))).style('opacity', 1);
}
  window.applyParagraphFocus = applyParagraphFocus;

  // 3) Bind once
  if (!window._paraFocusBound) {
    window.addEventListener('story:focusParagraph', (evt) => {
      const ids = (evt && evt.detail && Array.isArray(evt.detail.nodeIds)) ? evt.detail.nodeIds : [];
      applyParagraphFocus(ids);
    });
    window.addEventListener('story:clearFocus', () => {
      applyParagraphFocus([]);
    });
    window._paraFocusBound = true;
  }

  // Optional: if a mapping was injected before graph loaded, you can manually trigger one to test:
  // window.dispatchEvent(new CustomEvent('story:focusParagraph', { detail: { paragraphId: '...', nodeIds: ['n1','n2'] }}));
})();

      // --- Focus hook + event bridge (NEW) ---
      window.__CY_FOCUS__ = function(nodeIds) {
        if (typeof window.applyParagraphFocus === 'function') {
          window.applyParagraphFocus(Array.isArray(nodeIds) ? nodeIds : nodeIds);
        }
      };

      document.addEventListener('story:focusParagraph', (e) => {
        const ids = e?.detail?.nodeIds || [];
        if (typeof window.applyParagraphFocus === 'function') window.applyParagraphFocus(ids);
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
              const storyId = document.getElementById("storyIdInput").value;
              if (!storyId) { alert('Set Story ID first'); return; }
              fetch(`${API_BASE_URL}/struktur/${storyId}/${node.id()}`, { method: 'DELETE' })
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

      cy.on('select', 'node', (evt) => { openInspector(evt.target); });
      cy.on('unselect', 'node,edge', () => {
        if (cy.$('node:selected,edge:selected').length === 0) closeInspector();
      });

      // Add node mode: click on background to place new node
      cy.on('tap', (evt) => {
        if (evt.target === cy) {
          if (addNodeMode) {
            const p = evt.position;
            const id = (typeof crypto !== 'undefined' && crypto.randomUUID) ? crypto.randomUUID() : (Math.random()*1e17).toString(36);
            const storyId = storyInput.value.trim();
            if (!storyId) { alert('Set Story ID first'); return; }
            cy.add({ group:'nodes', data:{ id, label:'', type:'', time:'', color:'', detail:'' }, position:{ x:p.x, y:p.y } });
            // Update in-memory dataset
            const nodeObj = { id, label:'', type:'', time:'', color:'', detail:'', x:Math.round(p.x), y:Math.round(p.y), storyId, isNode:true };
            lastNodes.push(nodeObj);
            // Persist basic node immediately
            fetch(`${API_BASE_URL}/submit`, {
              method:'POST', headers:{'Content-Type':'application/json'},
              body: JSON.stringify({ storyId, nodes:[nodeObj], edges:[] })
            });
            const newNode = cy.$id(id);
            newNode.select();
            openInspector(newNode);
            newNode.data('hasXY', true);
            addNodeMode = false; addNodeBtn?.setAttribute('data-active','false');
          } else {
            // background click: close editor and clear any selection
            closeInspector();
            cy.$('node,edge').unselect();
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
      // (auto-layout for unsaved positions is now handled in reRender)
    }
    window.cy = cy;
  }

  function cyElements(nodes, edges) {
    const cyNodes = (nodes || []).map(n => {
      const canon = toCanonicalType(n.type);
      const color = (n.color && n.color.trim()) ? n.color : (TYPE_COLORS[canon] || '#666');

      // Coerce x/y to numbers (API may return them as strings)
      const xNum = (n.x !== undefined && n.x !== null) ? Number(n.x) : NaN;
      const yNum = (n.y !== undefined && n.y !== null) ? Number(n.y) : NaN;
      const hasXY = Number.isFinite(xNum) && Number.isFinite(yNum);

      const base = {
        data: {
          id: n.id,
          label: n.label,
          type: n.type || '',
          time: n.time || '',
          color,
          detail: n.detail || ''
        }
      };
      if (hasXY) {
        base.position = { x: xNum, y: yNum };
        base.data.hasXY = true;   // saved XY, but still draggable
      } else {
        base.data.hasXY = false;  // no saved XY yet
      }
      return base;
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
  if (loadBtn) {
    loadBtn.addEventListener("click", async (e) => {
      e.preventDefault();
      const storyId = storyInput.value.trim();
      if (!storyId) { alert("Please provide a Story ID"); return; }
      await loadStoryData(storyId);
      currentFilter = 'prozess';
      expandedNodeId = null;
      prevFilterBeforeExpand = null;
      if (filterTypeSelect) filterTypeSelect.value = 'prozess';
      needsLayout = true;
      reRender();
    });
  }

  if (storySelect) {
    (async () => {
      storySelect.disabled = true;
      try {
        const stories = await fetchStoryList();
        const placeholder = document.createElement('option');
        placeholder.value = '';
        placeholder.textContent = stories.length ? 'Select a story…' : 'No stories available';
        placeholder.disabled = stories.length > 0;
        storySelect.innerHTML = '';
        storySelect.appendChild(placeholder);
        stories.forEach(story => {
          const option = document.createElement('option');
          option.value = story.storyId;
          const school = story.schoolId ? ` (${story.schoolId})` : '';
          option.textContent = `${story.title || story.storyId}${school}`;
          storySelect.appendChild(option);
        });
        if (IS_STORY_MODE && STORY_ID) {
          storySelect.value = STORY_ID;
        } else {
          placeholder.selected = true;
        }
        storySelect.disabled = stories.length === 0;
      } catch (err) {
        console.error('Story list fetch failed', err);
        storySelect.innerHTML = '';
        const errorOption = document.createElement('option');
        errorOption.value = '';
        errorOption.textContent = 'Failed to load stories';
        storySelect.appendChild(errorOption);
        storySelect.disabled = true;
      }
    })();

    storySelect.addEventListener('change', (event) => {
      const selected = event.target.value;
      if (!selected) {
        ensureStoryRendered("", { forceReload: true });
        return;
      }
      ensureStoryRendered(selected, { forceReload: true });
    });
  }

  if (createBtn) {
    createBtn.addEventListener("click", (e) => {
      e.preventDefault();
      const storyId = storyInput.value;
      if (!storyId) { alert("Please provide a Story ID first."); return; }
      if (DEBUG_STATUS) {
        document.getElementById("status").textContent = `New story '${storyId}' created.`;
      }
      renderCytoscape([], []);
      lastNodes = [];
      lastEdges = [];
      currentFilter = 'prozess';
      expandedNodeId = null;
      prevFilterBeforeExpand = null;
      if (filterTypeSelect) filterTypeSelect.value = 'prozess';
      needsLayout = true;
      reRender();
    });
  }

  if (STORY_ID && storyInput && (!storyInput.value || storyInput.value !== STORY_ID)) {
    storyInput.value = STORY_ID;
  }

  window.addEventListener('popstate', () => {
    const nextId = extractStoryIdFromLocation();
    if (nextId) {
      ensureStoryRendered(nextId, { skipUrlUpdate: true, forceReload: true });
    } else {
      ensureStoryRendered("", { skipUrlUpdate: true, forceReload: true });
      applyPlaceholder();
    }
  });
});