#!/usr/bin/env node
// Sandbox attention hook (ADR-0011). Invoked by Claude Code hooks inside the
// container as: node status-writer.js <state>. Reads the hook payload on stdin,
// derives a best-effort "reason" (the pending question / last message), and
// writes the branch's status file under the shared .git so the host sees it.
const fs = require('fs');
const path = require('path');
const cp = require('child_process');

const state = process.argv[2] || 'working';
const branch = process.env.SR_SANDBOX;
if (!branch) process.exit(0); // not in a sandbox — no-op

let payload = '';
try { payload = fs.readFileSync(0, 'utf8'); } catch (e) {}

function extractReason(raw) {
  let j;
  try { j = JSON.parse(raw); } catch (e) { return ''; }
  // AskUserQuestion: the options the agent is waiting on.
  const ti = j.tool_input || {};
  if (Array.isArray(ti.questions) && ti.questions.length) {
    return ti.questions.map(q => q.question).filter(Boolean).join(' / ').slice(0, 300);
  }
  // Otherwise, the tail of the last assistant message from the transcript.
  if (j.transcript_path) {
    try {
      const lines = fs.readFileSync(j.transcript_path, 'utf8').trim().split('\n');
      for (let i = lines.length - 1; i >= 0; i--) {
        const e = JSON.parse(lines[i]);
        const msg = e.message || e;
        if (msg && msg.role === 'assistant') {
          const c = msg.content;
          const text = typeof c === 'string' ? c
            : Array.isArray(c) ? c.filter(b => b.type === 'text').map(b => b.text).join(' ')
            : '';
          if (text) return text.trim().slice(0, 300);
        }
      }
    } catch (e) {}
  }
  return '';
}

let gitCommon;
try {
  // execFileSync (no shell) — args passed directly, nothing interpolated.
  gitCommon = cp.execFileSync('git', ['rev-parse', '--git-common-dir'], { encoding: 'utf8' }).trim();
} catch (e) { process.exit(0); }
if (!path.isAbsolute(gitCommon)) gitCommon = path.resolve(process.cwd(), gitCommon);

const enc = branch.replace(/\//g, '%2F'); // matches Go EncodeBranch
const dir = path.join(gitCommon, '.stackr', 'sandboxes');
fs.mkdirSync(dir, { recursive: true });

const file = path.join(dir, enc + '.status');
const tmp = file + '.tmp';
const body = JSON.stringify({
  branch,
  state,
  reason: extractReason(payload),
  updatedAt: new Date().toISOString(),
}, null, 2) + '\n';
fs.writeFileSync(tmp, body);
fs.renameSync(tmp, file);
