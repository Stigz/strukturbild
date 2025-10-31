const React = window.React;
const marked = window.marked;

export default function ParagraphList({
  paragraphs,
  activeParagraphId,
  onSelectParagraph,
  onSaveParagraph,
  onAddParagraph,
  savingParagraphId,
}) {
  const [editingId, setEditingId] = React.useState(null);
  const [formState, setFormState] = React.useState({ title: "", bodyMd: "", citationsText: "" });
  const [formError, setFormError] = React.useState(null);

  const startEditing = React.useCallback(
    (paragraph) => {
      setEditingId(paragraph.paragraphId);
      setFormError(null);
      setFormState({
        title: paragraph.title || "",
        bodyMd: paragraph.bodyMd || "",
        citationsText: paragraph.citations && paragraph.citations.length > 0 ? JSON.stringify(paragraph.citations, null, 2) : "",
      });
    },
    []
  );

  const cancelEditing = React.useCallback(() => {
    setEditingId(null);
    setFormError(null);
  }, []);

  const handleSave = React.useCallback(
    async (paragraph) => {
      setFormError(null);
      let citations = paragraph.citations || [];
      if (formState.citationsText.trim()) {
        try {
          const parsed = JSON.parse(formState.citationsText);
          if (!Array.isArray(parsed)) {
            throw new Error("Citations must be an array");
          }
          citations = parsed;
        } catch (err) {
          setFormError(err.message || "Ungültige Zitate");
          return;
        }
      } else {
        citations = [];
      }
      const ok = await onSaveParagraph(paragraph.paragraphId, {
        title: formState.title,
        bodyMd: formState.bodyMd,
        citations,
      });
      if (ok !== false) {
        setEditingId(null);
      }
    },
    [formState, onSaveParagraph]
  );

  const moveParagraph = React.useCallback(
    (paragraph, direction) => {
      const newIndex = paragraph.index + direction;
      if (newIndex < 1) return;
      onSaveParagraph(paragraph.paragraphId, { index: newIndex });
    },
    [onSaveParagraph]
  );

  const maxIndex = React.useMemo(() => (paragraphs.length ? Math.max(...paragraphs.map((p) => p.index)) : 0), [paragraphs]);

  return (
    <div className="story-paragraph-list">
      {paragraphs.map((paragraph) => {
        const isActive = paragraph.paragraphId === activeParagraphId;
        const isEditing = editingId === paragraph.paragraphId;
        const citationText = paragraph.citations
          ? paragraph.citations
              .map((c) => `${c.transcriptId || "?"} ${Array.isArray(c.minutes) ? c.minutes.join(",") : ""}`)
              .join(" | ")
          : "";
        const html = marked
          ? marked.parse(paragraph.bodyMd || "", { mangle: false, headerIds: false })
          : (paragraph.bodyMd || "").replace(/\n/g, "<br>");
        return (
          <article
            key={paragraph.paragraphId}
            className={`story-paragraph-card${isActive ? " active" : ""}`}
            onClick={() => onSelectParagraph(paragraph.paragraphId)}
          >
            <header>
              <h2>
                {paragraph.index}. {paragraph.title || "(Ohne Titel)"}
              </h2>
              <div>
                <button type="button" onClick={(e) => { e.stopPropagation(); startEditing(paragraph); }}>
                  Bearbeiten
                </button>
                <button
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation();
                    moveParagraph(paragraph, -1);
                  }}
                  disabled={paragraph.index <= 1 || savingParagraphId === paragraph.paragraphId}
                >
                  ↑
                </button>
                <button
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation();
                    moveParagraph(paragraph, 1);
                  }}
                  disabled={paragraph.index >= maxIndex || savingParagraphId === paragraph.paragraphId}
                >
                  ↓
                </button>
              </div>
            </header>
            {isEditing ? (
              <div className="story-edit-area" onClick={(e) => e.stopPropagation()}>
                <input
                  type="text"
                  value={formState.title}
                  placeholder="Titel (optional)"
                  onChange={(e) => setFormState((state) => ({ ...state, title: e.target.value }))}
                />
                <textarea
                  value={formState.bodyMd}
                  onChange={(e) => setFormState((state) => ({ ...state, bodyMd: e.target.value }))}
                  placeholder="Markdown Text"
                />
                <textarea
                  value={formState.citationsText}
                  onChange={(e) => setFormState((state) => ({ ...state, citationsText: e.target.value }))}
                  placeholder='Zitate als JSON, z.B. [{"transcriptId":"...","minutes":[1,2]}]'
                />
                {formError && <div className="story-error">{formError}</div>}
                <div className="story-edit-actions">
                  <button
                    type="button"
                    className="primary"
                    disabled={savingParagraphId === paragraph.paragraphId}
                    onClick={() => handleSave(paragraph)}
                  >
                    Speichern
                  </button>
                  <button type="button" onClick={cancelEditing}>
                    Abbrechen
                  </button>
                </div>
              </div>
            ) : (
              <div>
                <div dangerouslySetInnerHTML={{ __html: html }} />
                {citationText && <div className="story-citation-list">Zitate: {citationText}</div>}
              </div>
            )}
          </article>
        );
      })}
      <button type="button" onClick={onAddParagraph} style={{ alignSelf: "flex-start" }}>
        + Abschnitt hinzufügen
      </button>
    </div>
  );
}
