import StoryPage from "/story/StoryPage.jsx";

const React = window.React;
const ReactDOM = window.ReactDOM;

function decodeComponentSafe(value) {
  if (typeof value !== "string" || value.length === 0) return "";
  try {
    return decodeURIComponent(value);
  } catch (err) {
    return value;
  }
}

function extractStoryIdFromLocation(loc = window.location) {
  if (!loc) return "";
  const pathname = loc.pathname || "";
  const pathMatch = pathname.match(/\/stories\/([^/]+)/);
  if (pathMatch && pathMatch[1]) {
    return decodeComponentSafe(pathMatch[1]);
  }
  try {
    const params = new URLSearchParams(loc.search || "");
    const queryStory = params.get("storyId") || params.get("storyID");
    if (queryStory) return queryStory;
  } catch (err) {
    // ignore URLSearchParams issues in unsupported environments
  }
  if (loc.hash) {
    const hashMatch = loc.hash.match(/story(?:Id)?=([^&]+)/i);
    if (hashMatch && hashMatch[1]) {
      return decodeComponentSafe(hashMatch[1]);
    }
  }
  return "";
}

if (React && ReactDOM) {
  let reactRoot = null;

  const renderStory = (storyId) => {
    const rootElement = document.getElementById("story-root");
    if (!rootElement) {
      return;
    }

    if (!storyId) {
      if (reactRoot && reactRoot.unmount) {
        reactRoot.unmount();
      } else if (ReactDOM.unmountComponentAtNode) {
        ReactDOM.unmountComponentAtNode(rootElement);
      }
      reactRoot = null;
      document.body.classList.remove("story-mode");
      rootElement.innerHTML = "";
      return;
    }

    document.body.classList.add("story-mode");
    const apiBaseUrl = (window.STRUKTURBILD_API_URL || "").replace(/\/$/, "");
    const element = React.createElement(StoryPage, { storyId, apiBaseUrl });

    if (ReactDOM.createRoot) {
      if (!reactRoot) {
        reactRoot = ReactDOM.createRoot(rootElement);
      }
      reactRoot.render(element);
    } else {
      ReactDOM.render(element, rootElement);
    }
  };

  window.renderStoryPage = renderStory;

  document.addEventListener("DOMContentLoaded", () => {
    const initialStoryId = extractStoryIdFromLocation();
    if (initialStoryId) {
      renderStory(initialStoryId);
    }
  });
}
