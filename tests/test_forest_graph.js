/* Regression test for the real-root (no synthetic REPERTOIRE node) graph layout.
 * Run from the project root: node tests/test_forest_graph.js
 * No packages or network required.
 */
'use strict';
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const vm = require('node:vm');
const src = fs.readFileSync(path.join(__dirname, '..', 'dashboard_template.html'), 'utf8');
const start = src.indexOf('function visibleChildren(id){');
const end = src.indexOf('// Positions are shown only inside', start);
assert.ok(start !== -1 && end > start, 'Cannot extract current graph layout functions');
const functions = src.slice(start, end);
const initialFen = 'rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1';
const fenAfterWhite = 'rnbqkbnr/pppppppp/8/8/4P3/8/PPPP1PPP/RNBQKBNR b KQkq - 0 1';
const fenAfterBlack = 'rnbqkbnr/pp1ppppp/2p5/8/4P3/8/PPPP1PPP/RNBQKBNR w KQkq - 0 2';
const nodes = [
 {id:0,parent:null,children:[1],fen:initialFen,turn:'white',comment:'',starting_comment:'',nags:[]},
 {id:1,parent:0,children:[2],fen:fenAfterWhite,turn:'black',comment:'',starting_comment:'',nags:[]},
 {id:2,parent:1,children:[],fen:fenAfterBlack,turn:'white',comment:'',starting_comment:'',nags:[],practical_trap:{id:2}},
 {id:3,parent:null,children:[4],fen:initialFen,turn:'white',comment:'',starting_comment:'',nags:[]},
 {id:4,parent:3,children:[],fen:fenAfterWhite,turn:'black',comment:'',starting_comment:'',nags:[]},
];
const state = {
  N: nodes, R: [0,3], focus: 0, kept: new Set(nodes.map(n=>n.id)),
  expandedChains: new Set(), trapPath: new Set([0,1,2]), viewMode:'all',
  NODE_STEP: 166, NODE_X0:132, depthLimit:1350,
  $:id=>id==='graphDepth'?{value:'20'}:id==='collapseLinear'?{checked:false}:{checked:true},
  hasComment:n=>Boolean(n.comment||n.starting_comment),
  plyOf:n=>{const f=n.fen.split(' ');return 2*(Number(f[5])-1)+(f[1]==='b'?1:0)},
};
vm.createContext(state);
vm.runInContext(functions,state);
const all=state.graphSubset();
assert.deepEqual([...all.items.filter(n=>!all.edges.some(e=>e.b===n.id)).map(n=>n.id)],[0,3]);
assert.equal(all.items.length,5);
assert.equal(all.edges.length,3);
assert.ok(all.items.every(n=>n.id>=0),'Synthetic root found');
assert.notEqual(all.items.find(n=>n.id===0).y,all.items.find(n=>n.id===3).y);
assert.ok(Number.isFinite(all.width) && Number.isFinite(all.height));
state.viewMode='traps';
const traps=state.graphSubset();
assert.deepEqual([...traps.items.map(x=>x.id)],[0,1,2]);
assert.equal(traps.edges.length,2);
state.trapPath=new Set();
const empty=state.graphSubset();
assert.equal(empty.items.length,0);
assert.ok(Number.isFinite(empty.width) && Number.isFinite(empty.height));
state.viewMode='chapter';state.focus=3;
const chapter=state.graphSubset();
assert.deepEqual([...chapter.items.map(n=>n.id)],[3,4]);
console.log('PASS: independent chapter roots, trap view, empty trap view, chapter focus and finite layouts.');
