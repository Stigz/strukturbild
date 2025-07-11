const API_BASE_URL = "https://qhbbu19wz1.execute-api.us-east-1.amazonaws.com";


// Ensure submitBtn event listener is attached after DOM is fully loaded
document.addEventListener("DOMContentLoaded", () => {
  const form = document.getElementById("elementForm");
  form.addEventListener("submit", async (e) => {
    e.preventDefault();

    const personId = document.getElementById("elementPersonIdInput").value;
    const type = document.getElementById("typeSelect").value;
    const label = document.getElementById("labelInput").value;
    const from = document.getElementById("fromInput").value;
    const to = document.getElementById("toInput").value;
    const x = parseInt(document.getElementById("xInput").value, 10) || 0;
    const y = parseInt(document.getElementById("yInput").value, 10) || 0;
    const id = (typeof crypto !== "undefined" && crypto.randomUUID) ? crypto.randomUUID() : (Math.random() * 1e17).toString(36);

    document.getElementById("generatedId").value = id;

    const payload = {
      personId,
      title: "",
      nodes: type === "node" ? [{ id, label, x, y, personId, isNode: true }] : [],
      edges: type === "edge" ? [{ from, to, label }] : []
    };

    const response = await fetch(`${API_BASE_URL}/submit`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload)
    });

    const text = await response.text();
    document.getElementById("status").innerText = text;
  });

  const loadBtn = document.getElementById("loadBtn");
  if (loadBtn) {
    loadBtn.addEventListener("click", async (e) => {
      e.preventDefault();
      const personId = document.getElementById("personIdInput").value;
      const status = document.getElementById("status");

      try {
        const response = await fetch(`${API_BASE_URL}/struktur/${personId}`);
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
            const wrapper = document.createElement("div");
            wrapper.style.margin = "0.5em";

            const idText = document.createElement("div");
            idText.textContent = `ID: ${node.id}`;
            idText.style.fontSize = "0.75em";
            idText.style.color = "#666";

            const box = document.createElement("div");
            box.textContent = node.label;
            box.style.border = "1px solid #333";
            box.style.padding = "0.5em";
            box.style.background = "#eef";

            wrapper.appendChild(idText);
            wrapper.appendChild(box);
            renderArea.appendChild(wrapper);
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
  }
});