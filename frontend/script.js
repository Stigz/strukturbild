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