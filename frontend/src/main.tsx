import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import "./index.css";
import App from "./App.tsx";
import { initLogger } from "./services/logger";
import { startSyncer } from "./services/syncer";

// Initialize client-side logger and sync loop
initLogger();
startSyncer();

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
