const API_BASE_URL = "https://qhbbu19wz1.execute-api.us-east-1.amazonaws.com";

document.addEventListener("DOMContentLoaded", () => {
  const form = document.getElementById("elementForm");
  const loadBtn = document.getElementById("loadBtn");
  let cy;

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

    await fetch(`${API_BASE_URL}/submit`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload)
    });

    loadPersonData(personId);
  });

  loadBtn.addEventListener("click", async (e) => {
    e.preventDefault();
    const personId = document.getElementById("personIdInput").value;
    loadPersonData(personId);
  });

  async function loadPersonData(personId) {
    const status = document.getElementById("status");
    try {
      const response = await fetch(`${API_BASE_URL}/struktur/${personId}`);
      if (!response.ok) {
        const errorText = await response.text();
        throw new Error(`Server error ${response.status}: ${errorText}`);
      }
      const text = await response.text();
      const data = JSON.parse(text);
      status.textContent = JSON.stringify(data, null, 2);
      const nodes = data.nodes || [];
      const edges = data.edges || [];
      renderCytoscape(nodes, edges);
    } catch (err) {
      console.error("Failed to load strukturbild:", err);
      status.textContent = "Error loading strukturbild";
    }
  }

  function renderCytoscape(nodes, edges) {
    if (!cy) {
      cy = cytoscape({
        container: document.getElementById("renderArea"),
        style: [
          {
            selector: 'node',
            style: {
              'label': 'data(label)',
              'text-valign': 'center',
              'text-halign': 'center',
              'background-color': '#666',
              'color': '#fff',
              'width': 50,
              'height': 50,
              'font-size': 12
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
        elements: []
      });
    } else {
      cy.elements().remove();
    }

    const cyNodes = nodes.map(n => ({
      data: { id: n.id, label: n.label }
    }));

    const cyEdges = edges
      .filter(e => e.from && e.to)
      .map(e => ({
        data: {
          id: `${e.from}-${e.to}`,
          source: e.from,
          target: e.to,
          label: e.label || ''
        }
      }));

    console.log("Adding nodes:", cyNodes);
    console.log("Adding edges:", cyEdges);
    cy.add([...cyNodes, ...cyEdges]);
    console.log("Cytoscape graph elements after add:", cy.elements().map(ele => ele.data()));
    cy.layout({ name: 'cose' }).run();
  }
});