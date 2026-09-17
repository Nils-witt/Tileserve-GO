import { useEffect, useState } from "react";
import { fetchBuildInfo } from "../lib/version";

export default function Footer() {
  const [buildInfo, setBuildInfo] = useState("");

  useEffect(() => {
    fetchBuildInfo()
      .then(setBuildInfo)
      .catch(() => {});
  }, []);

  return (
    <footer>
      &copy; 2026 Witt, Nils &middot; Tileserve-GO &middot;{" "}
      <a href="https://github.com/Nils-witt/Tileserve-GO" target="_blank" rel="noopener noreferrer">
        GitHub
      </a>
      {buildInfo}
    </footer>
  );
}
