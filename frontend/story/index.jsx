/* global React, ReactDOM, marked */
(() => {
  const { useEffect, useState, useCallback, useRef } = React;

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
    const [actionError, setActionError] = useState("");
    const [editingParagraphId, setEditingParagraphId] = useState(null);
    const [isNewParagraph, setIsNewParagraph] = useState(false);
    const [draftParagraph, setDraftParagraph] = useState({ index: "", title: "", bodyMd: "" });
    const [savingParagraph, setSavingParagraph] = useState(false);
    const [nodeMapDrafts, setNodeMapDrafts] = useState({});
    const [savingNodesFor, setSavingNodesFor] = useState(null);

    const paragraphRefs = useRef({});
    const allowScrollFocusRef = useRef(true);
    const focusRestoreTimeoutRef = useRef(null);
    const skipNextAutoFocusRef = useRef(false);
    const focusedRef = useRef(focusedParaId);
    const pendingScrollRef = useRef(false);

    useEffect(() => {
      focusedRef.current = focusedParaId;
    }, [focusedParaId]);

    const handleParagraphRef = useCallback((paraId, node) => {
      if (!paragraphRefs.current) paragraphRefs.current = {};
      if (node) {
        paragraphRefs.current[paraId] = node;
      } else {
        delete paragraphRefs.current[paraId];
      }
    }, []);

    const pickCenteredParagraph = useCallback(() => {
      if (!paragraphRefs.current) return null;
      const viewportHeight = window.innerHeight || document.documentElement?.clientHeight || 0;
      if (!viewportHeight) return null;
      const viewportCenter = viewportHeight / 2;
      let bestId = null;
      let bestDistance = Infinity;
      Object.entries(paragraphRefs.current).forEach(([pid, node]) => {
        if (!node || typeof node.getBoundingClientRect !== "function") return;
        const rect = node.getBoundingClientRect();
        if (rect.bottom <= 0 || rect.top >= viewportHeight) return;
        const paraCenter = rect.top + (rect.height / 2);
        const distance = Math.abs(paraCenter - viewportCenter);
        if (distance < bestDistance) {
          bestDistance = distance;
          bestId = pid;
        }
      });
      return bestId;
    }, []);

    const restoreAutoFocus = useCallback(() => {
      allowScrollFocusRef.current = true;
      focusRestoreTimeoutRef.current = null;
      if (skipNextAutoFocusRef.current) {
        skipNextAutoFocusRef.current = false;
        return;
      }
      const candidate = pickCenteredParagraph();
      if (candidate && focusedRef.current !== candidate) {
        setFocusedParaId(candidate);
      }
    }, [pickCenteredParagraph]);

    const temporarilyDisableAutoFocus = useCallback(() => {
      allowScrollFocusRef.current = false;
      if (focusRestoreTimeoutRef.current) {
        window.clearTimeout(focusRestoreTimeoutRef.current);
      }
      focusRestoreTimeoutRef.current = window.setTimeout(restoreAutoFocus, 600);
    }, [restoreAutoFocus]);

    useEffect(() => () => {
      if (focusRestoreTimeoutRef.current) {
        window.clearTimeout(focusRestoreTimeoutRef.current);
      }
    }, []);

    const handleParagraphClick = useCallback((paraId) => {
      const isAlreadyFocused = focusedRef.current === paraId;
      if (isAlreadyFocused) {
        skipNextAutoFocusRef.current = true;
        setFocusedParaId(null);
        temporarilyDisableAutoFocus();
        return;
      }
      skipNextAutoFocusRef.current = false;
      temporarilyDisableAutoFocus();
      pendingScrollRef.current = true;
      setFocusedParaId(paraId);
    }, [temporarilyDisableAutoFocus]);

    useEffect(() => {
      if (!focusedParaId) {
        pendingScrollRef.current = false;
        return;
      }
      if (!pendingScrollRef.current) return;

      let cancelled = false;

      function scrollToParagraph() {
        if (cancelled) return;
        const el = paragraphRefs.current?.[focusedParaId] || null;
        if (!el) {
          pendingScrollRef.current = false;
          return;
        }

        let scrolled = false;
        if (typeof el.scrollIntoView === "function") {
          try {
            el.scrollIntoView({ behavior: "smooth", block: "center" });
            scrolled = true;
          } catch (err) {
            try {
              el.scrollIntoView(true);
              scrolled = true;
            } catch (err2) {
              // ignore and fall back to manual scroll
            }
          }
        }

        if (!scrolled && typeof window !== "undefined") {
          const rect = typeof el.getBoundingClientRect === "function" ? el.getBoundingClientRect() : null;
          if (rect) {
            const doc = typeof document !== "undefined" ? document : null;
            const viewportHeight = window.innerHeight || doc?.documentElement?.clientHeight || 0;
            const offset = viewportHeight ? (viewportHeight / 2) - (rect.height / 2) : 0;
            const targetTop = (window.pageYOffset || window.scrollY || 0) + rect.top - offset;
            try {
              window.scrollTo({ top: targetTop, behavior: "smooth" });
              scrolled = true;
            } catch (err3) {
              window.scrollTo(0, targetTop);
              scrolled = true;
            }
          }
        }

        pendingScrollRef.current = false;
      }

      const scheduleScroll = () => {
        if (cancelled) return;
        if (typeof window !== "undefined" && typeof window.requestAnimationFrame === "function") {
          window.requestAnimationFrame(() => {
            if (cancelled) return;
            window.requestAnimationFrame(() => {
              if (cancelled) return;
              scrollToParagraph();
            });
          });
        } else {
          scrollToParagraph();
        }
      };

      scheduleScroll();

      return () => {
        cancelled = true;
      };
    }, [focusedParaId]);

    useEffect(() => {
      function handleScrollFocus() {
        if (!allowScrollFocusRef.current) return;
        const candidate = pickCenteredParagraph();
        if (candidate && focusedRef.current !== candidate) {
          setFocusedParaId(candidate);
        }
      }
      window.addEventListener("scroll", handleScrollFocus, { passive: true });
      handleScrollFocus();
      return () => {
        window.removeEventListener("scroll", handleScrollFocus);
      };
    }, [pickCenteredParagraph]);

    useEffect(() => {
      if (!allowScrollFocusRef.current) return;
      const candidate = pickCenteredParagraph();
      if (candidate && focusedRef.current !== candidate) {
        setFocusedParaId(candidate);
      }
    }, [paragraphs, pickCenteredParagraph]);

    const reloadStory = useCallback(async () => {
      if (!storyId) return;
      setLoading(true);
      setError("");
      setActionError("");
      try {
        const url = `${API_BASE}/api/stories/${encodeURIComponent(storyId)}/full`;
        const res = await fetch(url);
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        const data = await res.json();

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
        setNodeMapDrafts(() => {
          const draft = {};
          if (mapping && typeof mapping === "object") {
            Object.entries(mapping).forEach(([pid, ids]) => {
              if (Array.isArray(ids)) {
                draft[pid] = ids.join(", ");
              }
            });
          }
          return draft;
        });
        setFocusedParaId(prev => (prev && paras.some(p => p.paragraphId === prev) ? prev : null));
      } catch (e) {
        setError(String(e.message || e));
      } finally {
        setLoading(false);
      }
    }, [storyId]);

    // Fetch story on mount / storyId change
    useEffect(() => {
      if (storyId) {
        reloadStory();
      }
    }, [storyId, reloadStory]);

    const effectiveStoryId = story?.storyId ?? story?.story?.storyId ?? storyId;

    function startCreateParagraph() {
      const nextIndex = paragraphs.reduce((max, p) => Math.max(max, p.index || 0), 0) + 1;
      setEditingParagraphId("new");
      setIsNewParagraph(true);
      setDraftParagraph({ index: String(nextIndex), title: "", bodyMd: "" });
      setActionError("");
    }

    function startEditParagraph(para) {
      if (!para) return;
      setEditingParagraphId(para.paragraphId);
      setIsNewParagraph(false);
      setDraftParagraph({ index: String(para.index || ""), title: para.title || "", bodyMd: para.bodyMd || "" });
      setActionError("");
    }

    function cancelParagraphEdit() {
      setEditingParagraphId(null);
      setIsNewParagraph(false);
      setDraftParagraph({ index: "", title: "", bodyMd: "" });
      setSavingParagraph(false);
    }

    function updateDraft(field, value) {
      setDraftParagraph(prev => ({ ...prev, [field]: value }));
    }

    async function handleSaveParagraph(ev) {
      ev.preventDefault();
      if (savingParagraph) return;
      const sid = effectiveStoryId;
      if (!sid) {
        setActionError("Missing story id.");
        return;
      }
      const idx = parseInt(draftParagraph.index, 10);
      if (!Number.isInteger(idx) || idx < 1) {
        setActionError("Index must be a positive integer.");
        return;
      }
      setSavingParagraph(true);
      setActionError("");
      const titleValue = (draftParagraph.title || "").trim();
      const bodyValue = draftParagraph.bodyMd || "";
      try {
        if (isNewParagraph) {
          const createPayload = {
            index: idx,
            title: titleValue,
            bodyMd: bodyValue,
          };
          const res = await fetch(`${API_BASE}/api/stories/${encodeURIComponent(sid)}/paragraphs`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(createPayload),
          });
          if (!res.ok) throw new Error(`HTTP ${res.status}`);
          let targetId = null;
          try {
            const data = await res.json();
            targetId = data && data.id ? data.id : null;
          } catch (err) {
            targetId = null;
          }
          await reloadStory();
          if (targetId) {
            pendingScrollRef.current = true;
            setFocusedParaId(targetId);
          }
        } else {
          const updatePayload = {
            storyId: sid,
            index: idx,
            title: titleValue,
            bodyMd: bodyValue,
          };
          const res = await fetch(`${API_BASE}/api/paragraphs/${encodeURIComponent(editingParagraphId)}`, {
            method: "PATCH",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(updatePayload),
          });
          if (!res.ok) throw new Error(`HTTP ${res.status}`);
          let targetId = editingParagraphId;
          try {
            const data = await res.json();
            if (data && data.id) targetId = data.id;
          } catch (err) {
            // ignore JSON parse issues
          }
          await reloadStory();
          if (targetId) {
            pendingScrollRef.current = true;
            setFocusedParaId(targetId);
          }
        }
        cancelParagraphEdit();
      } catch (e) {
        setActionError(String(e.message || e));
      } finally {
        setSavingParagraph(false);
      }
    }

    function getNodeDraftValue(paragraphId) {
      if (Object.prototype.hasOwnProperty.call(nodeMapDrafts, paragraphId)) {
        return nodeMapDrafts[paragraphId];
      }
      const existing = story?.paragraphNodeMap?.[paragraphId];
      return Array.isArray(existing) ? existing.join(", ") : "";
    }

    function handleNodeMapInput(paragraphId, value) {
      setNodeMapDrafts(prev => ({ ...prev, [paragraphId]: value }));
    }

    function resetNodeDraft(paragraphId) {
      const existing = story?.paragraphNodeMap?.[paragraphId];
      setNodeMapDrafts(prev => ({
        ...prev,
        [paragraphId]: Array.isArray(existing) ? existing.join(", ") : "",
      }));
    }

    function parseNodeDraft(paragraphId) {
      const raw = getNodeDraftValue(paragraphId);
      if (!raw) return [];
      const pieces = raw.split(/[\s,]+/).map(part => part.trim()).filter(Boolean);
      const seen = new Set();
      const unique = [];
      pieces.forEach((item) => {
        if (!seen.has(item)) {
          seen.add(item);
          unique.push(item);
        }
      });
      return unique;
    }

    async function handleSaveNodeMap(paragraphId) {
      if (savingNodesFor === paragraphId) return;
      const sid = effectiveStoryId;
      if (!sid) {
        setActionError("Missing story id.");
        return;
      }
      setSavingNodesFor(paragraphId);
      setActionError("");
      try {
        const items = parseNodeDraft(paragraphId);
        const existing = story?.paragraphNodeMap || {};
        const nextMap = {};
        Object.entries(existing).forEach(([pid, ids]) => {
          if (Array.isArray(ids) && ids.length) {
            nextMap[pid] = ids.slice();
          }
        });
        if (items.length) {
          nextMap[paragraphId] = items;
        } else {
          delete nextMap[paragraphId];
        }
        const res = await fetch(`${API_BASE}/api/stories/${encodeURIComponent(sid)}`, {
          method: "PATCH",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ paragraphNodeMap: nextMap }),
        });
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        await reloadStory();
        pendingScrollRef.current = true;
        setFocusedParaId(paragraphId);
      } catch (e) {
        setActionError(String(e.message || e));
      } finally {
        setSavingNodesFor(null);
      }
    }

    // Apply focus to the graph whenever selection changes
    useEffect(() => {
      const sid = story?.storyId ?? story?.story?.storyId ?? storyId;
      const storyMap = (story && story.paragraphNodeMap) || null;
      const ids = (storyMap && storyMap[focusedParaId])
        || (sid && window.__PARA_NODE_MAP__?.[sid]?.[focusedParaId])
        || [];
      window.applyParagraphFocus?.(ids);
    }, [focusedParaId, story, storyId]);

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

    const editingActive = editingParagraphId !== null;

    return (
      <div className="story-paragraphs-column">
        <div style={{ padding: "4px 2px 10px" }}>
          <h2 style={{ margin: 0, fontSize: "1.15rem" }}>{story?.title || storyId}</h2>
          <div style={{ display: "flex", gap: "8px", flexWrap: "wrap", marginTop: "8px" }}>
            <button
              type="button"
              onClick={startCreateParagraph}
              disabled={savingParagraph}
              style={{ padding: "6px 12px", fontSize: "0.85rem" }}
            >
              Add paragraph
            </button>
            {editingActive ? (
              <button
                type="button"
                onClick={cancelParagraphEdit}
                style={{ padding: "6px 12px", fontSize: "0.85rem" }}
              >
                Cancel edit
              </button>
            ) : null}
          </div>
          {actionError ? <p className="story-error" style={{ marginTop: "6px" }}>{actionError}</p> : null}
        </div>

        {editingActive ? (
          <form
            className="story-paragraph-editor"
            onSubmit={handleSaveParagraph}
            style={{ background: "#f5f7fb", border: "1px solid rgba(0,0,0,0.08)", padding: "12px", borderRadius: "8px", margin: "0 0 12px" }}
          >
            <div style={{ display: "grid", gridTemplateColumns: "90px 1fr", gap: "12px" }}>
              <label style={{ display: "flex", flexDirection: "column", fontSize: "0.8rem", color: "#444" }}>
                Index
                <input
                  type="number"
                  min="1"
                  value={draftParagraph.index}
                  onChange={ev => updateDraft("index", ev.target.value)}
                  style={{ marginTop: "4px", padding: "6px", fontSize: "0.9rem" }}
                  required
                />
              </label>
              <label style={{ display: "flex", flexDirection: "column", fontSize: "0.8rem", color: "#444" }}>
                Title
                <input
                  type="text"
                  value={draftParagraph.title}
                  onChange={ev => updateDraft("title", ev.target.value)}
                  style={{ marginTop: "4px", padding: "6px", fontSize: "0.9rem" }}
                />
              </label>
            </div>
            <label style={{ display: "flex", flexDirection: "column", fontSize: "0.8rem", color: "#444", marginTop: "10px" }}>
              Body (Markdown)
              <textarea
                value={draftParagraph.bodyMd}
                onChange={ev => updateDraft("bodyMd", ev.target.value)}
                style={{ marginTop: "4px", padding: "6px", fontSize: "0.9rem", minHeight: "140px" }}
              />
            </label>
            <div style={{ display: "flex", justifyContent: "flex-end", gap: "8px", marginTop: "12px" }}>
              <button type="button" onClick={cancelParagraphEdit} style={{ padding: "6px 12px" }}>
                Cancel
              </button>
              <button type="submit" disabled={savingParagraph} style={{ padding: "6px 12px" }}>
                {savingParagraph ? "Saving…" : "Save paragraph"}
              </button>
            </div>
          </form>
        ) : null}

        <div className="story-paragraph-list">
          {paragraphs.map(p => {
            const active = p.paragraphId === focusedParaId;
            const html = (typeof marked !== "undefined" && p.bodyMd)
              ? marked.parse(p.bodyMd)
              : (p.bodyMd || "");
            const nodeDraftValue = getNodeDraftValue(p.paragraphId);
            const trimmedTitle = (p.title || "").trim();
            return (
              <article
                key={p.paragraphId}
                className={`story-paragraph-card${active ? " active" : ""}`}
                onClick={() => handleParagraphClick(p.paragraphId)}
                data-paragraph-id={p.paragraphId}
                ref={node => handleParagraphRef(p.paragraphId, node)}
              >
                <header style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: "8px" }}>
                  <h2 style={{ margin: 0 }}>
                    §{p.index || "?"}
                    {trimmedTitle ? `: ${trimmedTitle}` : ""}
                  </h2>
                  <button
                    type="button"
                    onClick={ev => { ev.stopPropagation(); startEditParagraph(p); }}
                    style={{ padding: "4px 8px", fontSize: "0.75rem" }}
                  >
                    Edit
                  </button>
                </header>
                <div
                  className="story-paragraph-body"
                  dangerouslySetInnerHTML={{ __html: html }}
                />
                {renderCitations(p)}
                {active ? (
                  <div
                    className="story-node-map-editor"
                    style={{ marginTop: "10px", padding: "8px", background: "#f6f8fb", borderRadius: "6px" }}
                    onClick={ev => ev.stopPropagation()}
                  >
                    <label style={{ display: "flex", flexDirection: "column", fontSize: "0.78rem", color: "#333" }}>
                      Node IDs (comma or space separated)
                      <input
                        type="text"
                        value={nodeDraftValue}
                        onChange={ev => handleNodeMapInput(p.paragraphId, ev.target.value)}
                        style={{ marginTop: "4px", padding: "6px", fontSize: "0.85rem" }}
                      />
                    </label>
                    <div style={{ display: "flex", justifyContent: "flex-end", gap: "8px", marginTop: "8px" }}>
                      <button
                        type="button"
                        onClick={ev => { ev.stopPropagation(); resetNodeDraft(p.paragraphId); }}
                        style={{ padding: "4px 8px", fontSize: "0.75rem" }}
                      >
                        Reset
                      </button>
                      <button
                        type="button"
                        onClick={ev => { ev.stopPropagation(); handleSaveNodeMap(p.paragraphId); }}
                        disabled={savingNodesFor === p.paragraphId}
                        style={{ padding: "4px 8px", fontSize: "0.75rem" }}
                      >
                        {savingNodesFor === p.paragraphId ? "Saving…" : "Save node links"}
                      </button>
                    </div>
                  </div>
                ) : null}
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