import { test, before } from 'node:test';
import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { mkdtemp, mkdir, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { Store, ConflictError, InvalidTreeError } from './store.js';

const exec = promisify(execFile);
let remote;
let root;

// A bare repo shaped like romaine-life/at: two commands, a menu running both, a bookmarks tree.
before(async () => {
  root = await mkdtemp(path.join(tmpdir(), 'fzt-store-'));
  const seed = path.join(root, 'seed');
  remote = path.join(root, 'remote.git');
  await mkdir(path.join(seed, 'commands'), { recursive: true });
  await mkdir(path.join(seed, 'trees'), { recursive: true });
  await writeFile(path.join(seed, 'commands', 'lightson.ps1'), 'lights on\n');
  await writeFile(path.join(seed, 'commands', 'launch.ps1'), 'launch\n');
  const menu = [
    { name: 'lights', children: [{ name: 'lightson' }] },
    { name: 'chrome', action: "launch 'Google Chrome' 'Chrome'" },
    { name: 'docs', url: 'https://example.com' },
  ];
  await writeFile(path.join(seed, 'trees', 'u-menu.json'), JSON.stringify({ version: 12, updatedAt: 'x', tree: menu }));
  await writeFile(path.join(seed, 'trees', 'u-bookmarks.json'), JSON.stringify({ version: 3, updatedAt: 'x', tree: [] }));
  const git = (...a) => exec('git', ['-C', seed, ...a]);
  await exec('git', ['init', '-q', '-b', 'main', seed]);
  await git('add', '.');
  await git('-c', 'user.name=t', '-c', 'user.email=t@t', 'commit', '-q', '-m', 'seed');
  await exec('git', ['clone', '-q', '--bare', seed, remote]);
});

const open = name => Store.open({ repoUrl: remote, dir: path.join(root, name) });

test('reads a tree, and a missing one as null', async () => {
  const store = await open('read');
  assert.equal((await store.read('u-menu')).version, 12);
  assert.equal(await store.read('nobody-bookmarks'), null);
});

test('a write with the current version commits the next one to main', async () => {
  const store = await open('write');
  const saved = await store.write('u-bookmarks', [{ name: 'a', url: 'https://a' }], 3, 'tester');
  assert.equal(saved.version, 4);
  const other = await open('write-check');
  assert.equal((await other.read('u-bookmarks')).version, 4);
});

test('a stale baseVersion is a conflict carrying the current tree', async () => {
  const store = await open('stale');
  await assert.rejects(store.write('u-bookmarks', [], 1, 'tester'), err => {
    assert.ok(err instanceof ConflictError);
    assert.ok(err.current.version >= 4);
    return true;
  });
});

test('a menu may not run a command the store lacks', async () => {
  const store = await open('unknown');
  const current = await store.read('u-menu');
  const tree = [...current.tree, { name: 'ghost' }];
  await assert.rejects(store.write('u-menu', tree, current.version, 'tester'), /'ghost'/);
});

test('a menu may not drop the last entry for a command', async () => {
  const store = await open('drop');
  const current = await store.read('u-menu');
  const tree = current.tree.filter(n => n.name !== 'lights');
  await assert.rejects(store.write('u-menu', tree, current.version, 'tester'), err => {
    assert.ok(err instanceof InvalidTreeError);
    assert.match(err.message, /'lightson' would have no menu entry/);
    return true;
  });
});

test('moving entries between folders is allowed', async () => {
  const store = await open('move');
  const current = await store.read('u-menu');
  const tree = [{ name: 'misc', children: [{ name: 'lightson' }, current.tree[1]] }, current.tree[2]];
  assert.equal((await store.write('u-menu', tree, current.version, 'tester')).version, current.version + 1);
});

test('a writer that lost the push race retries from the new main', async () => {
  const a = await open('race-a');
  const b = await open('race-b');
  await b.write('u-bookmarks', [{ name: 'b' }], undefined, 'b');
  // a still holds the older clone; its write without a baseVersion lands after b's.
  const saved = await a.write('u-bookmarks', [{ name: 'a' }], undefined, 'a');
  const check = await open('race-check');
  assert.deepEqual((await check.read('u-bookmarks')).tree, [{ name: 'a' }]);
  assert.equal((await check.read('u-bookmarks')).version, saved.version);
});

test('tree ids cannot leave the trees folder', async () => {
  const store = await open('ids');
  await assert.rejects(store.read('../commands/x'), InvalidTreeError);
});
