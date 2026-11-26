import fs from "node:fs";
import path from "node:path";

const root = path.resolve(import.meta.dirname, "..");
const source = path.join(root, ".farm", ".output", "public");
const dest = path.join(root, "out");

function copyClean(srcDir, dstDir) {
  fs.mkdirSync(dstDir, { recursive: true });
  for (const entry of fs.readdirSync(srcDir, { withFileTypes: true })) {
    if (entry.name.endsWith(".gz") || entry.name.endsWith(".br")) {
      continue;
    }
    const srcPath = path.join(srcDir, entry.name);
    const dstPath = path.join(dstDir, entry.name);
    if (entry.isDirectory()) {
      copyClean(srcPath, dstPath);
    } else if (entry.isFile()) {
      fs.copyFileSync(srcPath, dstPath);
    }
  }
}

if (!fs.existsSync(path.join(source, "index.html"))) {
  throw new Error(`Missing prerendered index.html at ${source}`);
}

fs.rmSync(dest, { recursive: true, force: true });
copyClean(source, dest);
console.log(`Exported static dashboard to ${dest}`);
