import { describe, it, expect } from "vitest";
import { scaffoldWorkspace } from "./scaffold";
import type { DocumentSummary } from "../api/types";

function doc(id: string): DocumentSummary {
  return { id, title: id, size: 1, mod_time: "", writable: true, too_large: false, large_warning: false };
}

describe("scaffoldWorkspace", () => {
  const docs = [
    doc("repos/site/README.md"),
    doc("repos/site/docs/guide.md"),
    doc("repos/site/api/ref.markdown"),
    doc("repos/site/src/main.go"), // not markdown, ignored
    doc("other/thing.md"), // outside the target dir
  ];

  it("scopes to the directory, detects extensions, and names groups", () => {
    const s = scaffoldWorkspace("repos/site", "/home/me/work", docs);

    expect(s.name).toBe("Site");
    expect(s.absPath).toBe("/home/me/work/repos/site");
    expect(s.configPath).toBe("/home/me/work/repos/site/athenaeum.toml");
    expect(s.hasMarkdown).toBe(true);

    // Both extensions are present under the directory.
    expect(s.configToml).toContain('"**/*.md"');
    expect(s.configToml).toContain('"**/*.markdown"');

    // Groups for sub-directories that contain Markdown, not for src/.
    expect(s.configToml).toContain('id = "docs"');
    expect(s.configToml).toContain('id = "api"');
    expect(s.configToml).not.toContain('id = "src"');

    // A committable registry entry with the absolute path.
    expect(s.registryEntry).toContain('name = "Site"');
    expect(s.registryEntry).toContain('path = "/home/me/work/repos/site"');
  });

  it("emits only the extensions present (no spurious markdown pattern)", () => {
    const s = scaffoldWorkspace("repos/site", "/root", [doc("repos/site/a.md")]);
    expect(s.configToml).toContain('"**/*.md"');
    expect(s.configToml).not.toContain('"**/*.markdown"');
  });

  it("reports when a directory holds no Markdown", () => {
    const s = scaffoldWorkspace("repos/empty", "/root", [doc("repos/empty/main.go")]);
    expect(s.hasMarkdown).toBe(false);
  });
});
