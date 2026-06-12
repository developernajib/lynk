import { useCallback, useEffect, useState, type FormEvent } from "react";

import type { Note } from "../gen/example/v1/example_pb";
import { notes } from "../transport";

export default function Notes() {
  const [items, setItems] = useState<Note[]>([]);
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [error, setError] = useState("");

  const refresh = useCallback(async () => {
    try {
      const response = await notes.listNotes({ limit: 50, offset: 0 });
      setItems(response.notes);
    } catch (err) {
      setError(err instanceof Error ? err.message : "request failed");
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  async function create(event: FormEvent) {
    event.preventDefault();
    setError("");
    try {
      await notes.createNote({ title, body });
      setTitle("");
      setBody("");
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "request failed");
    }
  }

  // The version from the last read rides along so a concurrent edit surfaces
  // as a conflict instead of a silent overwrite.
  async function rename(note: Note) {
    const next = window.prompt("New title", note.title);
    if (!next || next === note.title) return;
    setError("");
    try {
      await notes.updateNote({
        id: note.id,
        title: next,
        body: note.body,
        version: note.version,
      });
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "request failed");
    }
  }

  return (
    <main>
      <form className="row" onSubmit={create}>
        <input
          placeholder="Title"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          required
        />
        <input
          placeholder="Body"
          value={body}
          onChange={(e) => setBody(e.target.value)}
        />
        <button type="submit">Add note</button>
      </form>
      {error && <p className="error">{error}</p>}
      <ul className="notes">
        {items.map((note) => (
          <li key={note.id}>
            <div>
              <strong>{note.title}</strong>
              <span className="muted"> v{String(note.version)}</span>
            </div>
            <p>{note.body}</p>
            <button className="link" onClick={() => rename(note)}>
              Rename
            </button>
          </li>
        ))}
        {items.length === 0 && <li className="muted">No notes yet.</li>}
      </ul>
    </main>
  );
}
