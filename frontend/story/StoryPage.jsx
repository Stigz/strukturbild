import ParagraphList from "./ParagraphList.jsx";
import ImportButton from "./ImportButton.jsx";

const React = window.React;

function GraphPane({ storyId, apiBaseUrl, refreshToken }) {
  const containerRef = React.useRef(null);
  const cyRef = React.useRef(null);
  const [status, setStatus] = React.useState({ loading: true, error: null });

  React.useEffect(() => {
    const abort = { cancelled: false };
    const loadGraph = async () => {
      if (!storyId || !window.cytoscape) {
        setStatus({ loading: false, error: "Graph library unavailable." });
        return;
      }
      setStatus({ loading: true, error: null });
      try {
        const res = await fetch(`${apiBaseUrl}/struktur/${storyId}`);
        if (!res.ok) {
          throw new Error(`Graph load failed (${res.status})`);
        }
        const data = await res.json();
        if (abort.cancelled) return;
        if (cyRef.current) {
          cyRef.current.destroy();
          cyRef.current = null;
        }
        const elements = [];
        let nodesWithPositions = 0;
        const totalNodes = Array.isArray(data.nodes) ? data.nodes.length : 0;
        (data.nodes || []).forEach((node) => {
          const nodeElement = {
            data: {
              id: node.id,
              label: node.label,
              detail: node.detail,
              type: node.type,
            },
          };
          const parsedX =
            typeof node.x === "number" ? node.x : Number.parseFloat(node.x);
          const parsedY =
            typeof node.y === "number" ? node.y : Number.parseFloat(node.y);
          if (Number.isFinite(parsedX) && Number.isFinite(parsedY)) {
            nodeElement.position = { x: parsedX, y: parsedY };
            nodesWithPositions += 1;
          }
          elements.push(nodeElement);
        });
        (data.edges || []).forEach((edge, idx) => {
          elements.push({
            data: {
              id: edge.id || `${edge.from}-${edge.to}-${idx}`,
              source: edge.from,
              target: edge.to,
              label: edge.label,
            },
          });
        });
        const hasPresetPositions =
          totalNodes > 0 && nodesWithPositions === totalNodes;

        cyRef.current = window.cytoscape({
          container: containerRef.current,
          elements,
          style: [
            {
              selector: "node",
              style: {
                "background-color": "#2563eb",
                "label": "data(label)",
                "text-valign": "center",
                "text-halign": "center",
                "color": "#fff",
                "font-size": 12,
                "padding": "8px",
                "shape": "round-rectangle",
              },
            },
            {
              selector: "edge",
              style: {
                "curve-style": "bezier",
                "target-arrow-shape": "triangle",
                "width": 2,
                "line-color": "#94a3b8",
                "target-arrow-color": "#94a3b8",
                "label": "data(label)",
                "font-size": 10,
                "text-background-color": "#fff",
                "text-background-opacity": 0.7,
              },
            },
          ],
          layout: hasPresetPositions
            ? { name: "preset" }
            : { name: "cose", animate: false, fit: true },
        });
        if (cyRef.current) {
          cyRef.current.boxSelectionEnabled(false);
          cyRef.current.autoungrabify(true);
          cyRef.current.userZoomingEnabled(true);
          cyRef.current.resize();
          try {
            cyRef.current.fit(undefined, 32);
          } catch (err) {
            // ignore fit issues on initial render
          }
        }
        setStatus({ loading: false, error: null });
      } catch (err) {
        if (!abort.cancelled) {
          setStatus({ loading: false, error: err.message });
        }
      }
    };
    loadGraph();
    return () => {
      abort.cancelled = true;
      if (cyRef.current) {
        cyRef.current.destroy();
        cyRef.current = null;
      }
    };
  }, [storyId, apiBaseUrl, refreshToken]);

  return (
    <div className="story-graph-container" ref={containerRef}>
      {status.loading && <div className="story-graph-placeholder">Lade Strukturbild…</div>}
      {!status.loading && status.error && (
        <div className="story-graph-placeholder">{status.error}</div>
      )}
    </div>
  );
}

export default function StoryPage({ storyId, apiBaseUrl }) {
  const [data, setData] = React.useState(null);
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState(null);
  const [activeParagraphId, setActiveParagraphId] = React.useState(null);
  const [savingId, setSavingId] = React.useState(null);
  const [refreshToken, setRefreshToken] = React.useState(0);

  const fetchStory = React.useCallback(async () => {
    if (!storyId) return;
    setLoading(true);
    setError(null);
    try {
      const res = await fetch(`${apiBaseUrl}/api/stories/${storyId}/full`);
      if (!res.ok) {
        throw new Error(`Story not found (${res.status})`);
      }
      const json = await res.json();
      setData(json);
    } catch (err) {
      setError(err.message);
      setData(null);
    } finally {
      setLoading(false);
    }
  }, [storyId, apiBaseUrl]);

  React.useEffect(() => {
    fetchStory();
  }, [fetchStory]);

  React.useEffect(() => {
    if (!data || !Array.isArray(data.paragraphs)) {
      return;
    }
    if (!activeParagraphId) {
      if (data.paragraphs.length > 0) {
        setActiveParagraphId(data.paragraphs[0].paragraphId);
      }
      return;
    }
    const exists = data.paragraphs.some((p) => p.paragraphId === activeParagraphId);
    if (!exists && data.paragraphs.length > 0) {
      setActiveParagraphId(data.paragraphs[0].paragraphId);
    }
  }, [data, activeParagraphId]);

  const handleSaveParagraph = React.useCallback(
    async (paragraphId, updates) => {
      setSavingId(paragraphId);
      setError(null);
      try {
        const res = await fetch(`${apiBaseUrl}/api/paragraphs/${paragraphId}`, {
          method: "PATCH",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ ...updates, storyId }),
        });
        if (!res.ok) {
          const text = await res.text();
          throw new Error(text || "Speichern fehlgeschlagen");
        }
        await fetchStory();
        setRefreshToken((token) => token + 1);
        return true;
      } catch (err) {
        setError(err.message);
        return false;
      } finally {
        setSavingId(null);
      }
    },
    [apiBaseUrl, storyId, fetchStory]
  );

  const handleAddParagraph = React.useCallback(async () => {
    const nextIndex = data?.paragraphs?.length
      ? Math.max(...data.paragraphs.map((p) => p.index)) + 1
      : 1;
    try {
      const res = await fetch(`${apiBaseUrl}/api/stories/${storyId}/paragraphs`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ index: nextIndex, bodyMd: "", citations: [] }),
      });
      if (!res.ok) {
        const text = await res.text();
        throw new Error(text || "Abschnitt konnte nicht angelegt werden");
      }
      await fetchStory();
      setRefreshToken((token) => token + 1);
    } catch (err) {
      setError(err.message);
    }
  }, [apiBaseUrl, storyId, data, fetchStory]);

  const handleImported = React.useCallback(() => {
    fetchStory();
    setRefreshToken((token) => token + 1);
  }, [fetchStory]);

  const storyTitle = data?.story?.title || "Story";
  const storySchool = data?.story?.schoolId ? `Schule: ${data.story.schoolId}` : "";

  return (
    <div className="story-page">
      <header className="story-header">
        <div>
          <h1>{storyTitle}</h1>
          {storySchool && <div>{storySchool}</div>}
          {error && <div className="story-error">{error}</div>}
        </div>
        <div className="story-import-area">
          <ImportButton apiBaseUrl={apiBaseUrl} onImported={handleImported} />
          <div className="story-import-hint">JSON importiert Abschnitte &amp; Zitate</div>
        </div>
      </header>
      <div className="story-body">
        <section className="story-graph-column" aria-label="Strukturbild">
          <GraphPane storyId={storyId} apiBaseUrl={apiBaseUrl} refreshToken={refreshToken} />
        </section>
        <section className="story-paragraphs-column" aria-label="Story"> 
          {loading ? (
            <div>Lade Story…</div>
          ) : (
            <ParagraphList
              paragraphs={data?.paragraphs || []}
              activeParagraphId={activeParagraphId}
              onSelectParagraph={setActiveParagraphId}
              onSaveParagraph={handleSaveParagraph}
              onAddParagraph={handleAddParagraph}
              savingParagraphId={savingId}
            />
          )}
        </section>
      </div>
    </div>
  );
}
