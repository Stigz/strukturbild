const API_BASE_URL = "https://qhbbu19wz1.execute-api.us-east-1.amazonaws.com";

document.getElementById("submitBtn").onclick = async () => {
  const title = document.getElementById("titleInput").value;

  const response = await fetch(`${API_BASE_URL}/submit`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      title,
      nodes: [{ id: "1", label: "Leadership", x: 0, y: 0 }],
      edges: []
    })
  });

  const text = await response.text();
  document.getElementById("status").innerText = text;
};

document.getElementById("loadBtn").addEventListener("click", async () => {
  const id = document.getElementById("idInput").value;
  const status = document.getElementById("status");

  try {
    const response = await fetch(`${API_BASE_URL}/struktur/${id}`);
    if (!response.ok) {
      const errorText = await response.text();
      throw new Error(`Server error ${response.status}: ${errorText}`);
    }
    const text = await response.text();
    try {
      const data = JSON.parse(text);
      console.log("🎯 Loaded strukturbild:", data);
      status.textContent = JSON.stringify(data, null, 2);
      const renderArea = document.getElementById("renderArea") || (() => {
        const area = document.createElement("div");
        area.id = "renderArea";
        area.style.marginTop = "1em";
        area.style.display = "flex";
        area.style.flexWrap = "wrap";
        document.body.appendChild(area);
        return area;
      })();

      renderArea.innerHTML = "";
      data.nodes.forEach((node) => {
        const box = document.createElement("div");
        box.textContent = node.label;
        box.style.border = "1px solid #333";
        box.style.padding = "0.5em";
        box.style.margin = "0.5em";
        box.style.background = "#eef";
        renderArea.appendChild(box);
      });
    } catch (err) {
      console.error("Failed to parse response JSON:", err, "Raw text:", text);
      status.textContent = "Error parsing response";
    }
  } catch (err) {
    console.error("Failed to load strukturbild:", err);
    status.textContent = "Error loading strukturbild";
  }
});