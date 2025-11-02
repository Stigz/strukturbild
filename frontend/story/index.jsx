import StoryPage from "/story/StoryPage.jsx";

const React = window.React;
const ReactDOM = window.ReactDOM;

const isStoryMode = window.location.pathname.startsWith("/stories/");

if (isStoryMode && React && ReactDOM) {
  document.addEventListener("DOMContentLoaded", () => {
    document.body.classList.add("story-mode");
    const rootElement = document.getElementById("story-root");
    if (!rootElement) {
      return;
    }
    const segments = window.location.pathname.split("/").filter(Boolean);
    const storyId = segments[1] || "";
    if (!storyId) {
      rootElement.innerText = "Missing story ID in URL.";
      return;
    }
    const baseUrl = (window.STRUKTURBILD_API_URL || "").replace(/\/$/, "");
    if (ReactDOM.createRoot) {
      const root = ReactDOM.createRoot(rootElement);
      root.render(React.createElement(StoryPage, { storyId, apiBaseUrl: baseUrl }));
    } else {
      ReactDOM.render(React.createElement(StoryPage, { storyId, apiBaseUrl: baseUrl }), rootElement);
    }
  });
}
