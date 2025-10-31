const React = window.React;

export default function ImportButton({ apiBaseUrl, onImported }) {
  const inputRef = React.useRef(null);
  const [importing, setImporting] = React.useState(false);
  const [error, setError] = React.useState(null);

  const triggerFile = () => {
    setError(null);
    if (inputRef.current) {
      inputRef.current.value = "";
      inputRef.current.click();
    }
  };

  const handleFile = async (event) => {
    const file = event.target.files && event.target.files[0];
    if (!file) return;
    try {
      setImporting(true);
      const text = await file.text();
      JSON.parse(text); // validate JSON structure
      const res = await fetch(`${apiBaseUrl}/api/stories/import`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: text,
      });
      if (!res.ok) {
        const msg = await res.text();
        throw new Error(msg || "Import fehlgeschlagen");
      }
      onImported();
      setError(null);
    } catch (err) {
      setError(err.message || "Import fehlgeschlagen");
    } finally {
      setImporting(false);
    }
  };

  return (
    <div>
      <input
        ref={inputRef}
        type="file"
        accept="application/json"
        style={{ display: "none" }}
        onChange={handleFile}
      />
      <button type="button" onClick={triggerFile} disabled={importing}>
        {importing ? "Importiere…" : "JSON importieren"}
      </button>
      {error && <div className="story-error">{error}</div>}
    </div>
  );
}
