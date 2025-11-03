/* global React, ReactDOM, marked */
(() => {
  const { useEffect, useState } = React;

  // Util: safe decode & build API base
  const API_BASE = (window.STRUKTURBILD_API_URL || "").replace(/\/+$/, "");

  function decodeURIComponentSafe(value) {
    if (typeof value !== "string" || value.length === 0) return "";
    try { return decodeURIComponent(value); } catch { return value; }
  }

  // Resolve storyId from URL (?storyId=... or /stories/:id)
  function extractStoryIdFromLocation(loc = window.location) {
    try {
      const pathname = loc.pathname || "";
      const m = pathname.match(/\/stories\/([^/]+)/);
      if (m && m[1]) return decodeURIComponentSafe(m[1]);
      const params = new URLSearchParams(loc.search || "");
      return params.get("storyId") || "";
    } catch {
      return "";
    }
  }

  function StoryUI({ storyId }) {
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState("");
    const [story, setStory] = useState(null);
    const [paragraphs, setParagraphs] = useState([]);
    const [detailsByParagraph, setDetailsByParagraph] = useState({});
    const [focusedParaId, setFocusedParaId] = useState(null);

    // Fetch story on mount / storyId change
    useEffect(() => {
      let cancelled = false;
      async function run() {
        setLoading(true);
        setError("");
        try {
          const url = `${API_BASE}/api/stories/${encodeURIComponent(storyId)}/full`;
          const res = await fetch(url);
          if (!res.ok) throw new Error(`HTTP ${res.status}`);
          const data = await res.json();

          if (cancelled) return;

          const storyPayload = data.story || null;
          const mapping = (storyPayload && storyPayload.paragraphNodeMap)
            || data.paragraphNodeMap
            || window.__PARA_NODE_MAP__
            || null;

          setStory(storyPayload ? { ...storyPayload, paragraphNodeMap: mapping || storyPayload.paragraphNodeMap || null } : null);

          const paras = Array.isArray(data.paragraphs)
            ? data.paragraphs.slice().sort((a, b) => (a.index || 0) - (b.index || 0))
            : [];
          setParagraphs(paras);

          setDetailsByParagraph(data.detailsByParagraph || {});

          setFocusedParaId(null);
        } catch (e) {
          if (!cancelled) setError(String(e.message || e));
        } finally {
          if (!cancelled) setLoading(false);
        }
      }
      if (storyId) run();
      return () => { cancelled = true; };
    }, [storyId]);

    // Apply focus to the graph whenever selection changes
    
      useEffect(() => {
    const sid =
      story?.storyId ?? story?.story?.storyId; // handle both shapes
    const ids =
      story?.paragraphNodeMap?.[focusedParaId] ??
      window.__PARA_NODE_MAP__?.[sid]?.[focusedParaId] ??
      [];
    window.applyParagraphFocus?.(ids);
  }, [focusedParaId, story]);

    // Render helpers
    function renderCitations(p) {
      const list = Array.isArray(p.citations) ? p.citations : [];
      if (list.length === 0) return null;
      return (
        <div className="story-citation-list">
          {list.map((c, i) => {
            const range = Array.isArray(c.minutes) ? c.minutes.join("–") : "";
            return <span key={i}>{c.transcriptId}{range ? ` (${range}′)` : ""}{i < list.length - 1 ? " · " : ""}</span>;
          })}
        </div>
      );
    }

    function renderDetails(paraId) {
      const arr = detailsByParagraph[paraId] || [];
      if (!arr.length) return null;
      return (
        <div className="story-details-rail">
          <h3>Details</h3>
          {arr.map(d => (
            <div className="story-detail-item" key={d.detailId}>
              {d.kind === "quote" ? <em>{d.text}</em> : <span>{d.text}</span>}
            </div>
          ))}
        </div>
      );
    }

    if (!storyId) {
      return <div className="story-paragraphs-column"><p className="story-error">No story selected.</p></div>;
    }
    if (loading) {
      return <div className="story-paragraphs-column"><p>Loading…</p></div>;
    }
    if (error) {
      return <div className="story-paragraphs-column"><p className="story-error">Error: {error}</p></div>;
    }

    return (
      <div className="story-paragraphs-column">
        {/* Optional header */}
        <div style={{padding: "4px 2px 10px"}}>
          <h2 style={{margin: 0, fontSize: "1.15rem"}}>{story?.title || storyId}</h2>
        </div>

        {/* Vertical list of paragraph cards */}
        <div className="story-paragraph-list">
          {paragraphs.map(p => {
            const active = p.paragraphId === focusedParaId;
            const html = (typeof marked !== "undefined" && p.bodyMd)
              ? marked.parse(p.bodyMd)
              : (p.bodyMd || "");
            return (
              <article
                key={p.paragraphId}
                className={`story-paragraph-card${active ? " active" : ""}`}
                onClick={() => setFocusedParaId(focusedParaId === p.paragraphId ? null : p.paragraphId)}
              >
                <header>
                  <h2>§{p.index || "?"}</h2>
                  {/* you can add small actions here later */}
                </header>
                <div
                  className="story-paragraph-body"
                  dangerouslySetInnerHTML={{ __html: html }}
                />
                {renderCitations(p)}
                {active ? renderDetails(p.paragraphId) : null}
              </article>
            );
          })}
        </div>
      </div>
    );
  }

  // Public entry called by script.js (ensureStoryRendered → window.renderStoryPage)
  window.renderStoryPage = function renderStoryPage(storyIdFromUrl) {
    const storyId = storyIdFromUrl || extractStoryIdFromLocation();
    const mount = document.getElementById("story-root");
    if (!mount) return;
    // React 18 root
    if (!mount.__root) {
      mount.__root = ReactDOM.createRoot(mount);
    }
    mount.__root.render(<StoryUI storyId={storyId} />);
  };
})();