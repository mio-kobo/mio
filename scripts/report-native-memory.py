#!/usr/bin/env python3
"""Assemble M19c acceptance from immutable command records; never edits M19a/b."""
import argparse, hashlib, json, pathlib, subprocess
ROOT=pathlib.Path(__file__).resolve().parent.parent

def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def read(p):return json.loads((ROOT/p).read_text())
def ref(p):
 f=ROOT/p
 return {'path':p,'sha256':sha(f)}
def main():
 parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--update',action='store_true');args=parser.parse_args()
 output=ROOT/'benchmarks/native-gc/m19c-acceptance.json'
 if output.exists() and not args.update:raise SystemExit('report exists; use --update only for this M19c report')
 base='build/m19c-20261026-';checks=[]
 for name in ['full','race-targeted','full-race','mio']:
  path=base+name+'/result.json';d=read(path)
  checks.append({'name':name,'status':d['status'] if d['status']!='RUNNING' else 'BLOCKED',**ref(path),'argv':d['argv'],'exit_code':d.get('exit_code'),'log':base+name+'/output.log','log_sha256':sha(ROOT/(base+name+'/output.log')),'sources_unchanged':d.get('sources_unchanged'),'note':'Incomplete while running; never counted as PASS.' if d['status']=='RUNNING' else ''})
 checks+=read('build/m19c-20261026-report/extra-checks.json')
 fixedpath=base+'fixed-point/provenance.json';fixed=read(fixedpath)
 probe_path='build/m19c-20261025-pure-probe-final/result.json';probe=read(probe_path)
 probe_sources_unchanged=all((ROOT/path).is_file() and sha(ROOT/path)==digest for path,digest in probe['sources'].items())
 fixed_sources_unchanged=all(pathlib.Path(x['path']).is_file() and sha(pathlib.Path(x['path']))==x['sha256'] for x in read(base+'fixed-point/source-manifest.json'))
 comparisons=[];benchmarks=[]
 for label in ['benchmark','specialized']:
  path=base+label+'/report.json';p=read(path);benchmarks.append({'kind':label,'status':p['status'],**ref(path),'configurations':len(p['workloads']),'measured_samples':sum(not x['warmup'] for x in p.get('samples',[])),'warmups_per_configuration':p['warmups'],'iterations_per_configuration':p['iterations'],'commands':p['commands']})
  if label=='benchmark':
   def allocator_summary(entry):
    samples=[sample['metrics'] for sample in p['samples'] if sample.get('path')==entry['path'] and not sample['warmup'] and sample.get('metrics')]
    if not samples:return None
    holes=[m['heap_cursor']-(m['heap_live_bytes']-m.get('los_live_bytes',0)) for m in samples]
    result={'scope':'ordinary Arena cursor holes only; excludes LOS and metadata; not a claim about OS physical fragmentation','hole_bytes_min':min(holes),'hole_bytes_max':max(holes),'hole_fraction_max':max(h/m['heap_cursor'] if m['heap_cursor'] else 0 for h,m in zip(holes,samples))}
    for field in ['free_bytes','largest_free_block','free_blocks']:
     if all(field in m for m in samples):result[field]={'min':min(m[field] for m in samples),'max':max(m[field] for m in samples)}
    return result
   for current in p['workloads']:
    if current['revision']!='current':continue
    old=next(w for w in p['workloads'] if w['revision']=='baseline' and all(w[k]==current[k] for k in ['name','engine','observation']))
    comparisons.append({'name':current['name'],'engine':current['engine'],'observation':current['observation'],'baseline_image_sha256':old['sha256'],'current_image_sha256':current['sha256'],'wall_p50_change_percent':100*(current['wall_seconds']['p50']/old['wall_seconds']['p50']-1),'wall_p99_change_percent':100*(current['wall_seconds']['p99']/old['wall_seconds']['p99']-1),'cpu_p50_change_percent':100*(current['cpu_seconds_p50']/old['cpu_seconds_p50']-1),'baseline_gc':old['gc'],'current_gc':current['gc'],'baseline_peak_rss_bytes':old['max_peak_rss_bytes'],'current_peak_rss_bytes':current['max_peak_rss_bytes'],'memory':current.get('memory'),'baseline_allocator':allocator_summary(old),'current_allocator':allocator_summary(current)})
 historical=read('benchmarks/native-gc/m19b-20261021.json');race_history=[]
 for c in historical['checks']:
  if 'race' not in c.get('name',''):continue
  item=dict(c);f=ROOT/item.get('log','');item['historical_log_hash_matches']=f.is_file() and ('log_sha256' not in item or sha(f)==item['log_sha256']);race_history.append(item)
 finalrace=next(c for c in checks if c['name']=='full-race');race_text=(ROOT/finalrace['log']).read_text(errors='replace');timeouts=[line.strip() for line in race_text.splitlines() if 'panic: test timed out' in line or line.startswith('FAIL\t') or line.startswith('FAIL \t')]
 required=[c for c in checks if c['name']!='full-race']
 feature_ok=all(c['status']=='PASS' for c in required) and fixed['status']=='BOOTSTRAP_PASS' and probe['status']=='PASS' and probe_sources_unchanged and fixed_sources_unchanged and all(p['status']=='PASS' for p in benchmarks)
 modified=['internal/aot/aot.go','internal/aot/darwinarm64/stack.go','internal/aot/darwinarm64/gc_sweep.go','internal/aot/darwinarm64/gc_telemetry.go','internal/aot/darwinarm64/m11_object_gc.go','internal/aot/darwinarm64/m12_array.go','internal/aot/darwinarm64/m19b_generational.go','internal/aot/darwinarm64/m19c_config.go','internal/aot/darwinarm64/m19c_integration.go','internal/aot/darwinarm64/m19c_los.go','internal/aot/darwinarm64/m19c_memory.go','internal/aot/darwinarm64/m19c_los_test.go','internal/aot/darwinarm64/m19c_memory_test.go','internal/aot/m19_observation_test.go','internal/aot/m19b_generational_test.go','internal/aot/m19c_memory_test.go','bunko/kiso/aot/lower/lower.mio','bunko/kiso/aot/lower/generational.mio','bunko/kiso/aot/lower/observation.mio','bunko/kiso/aot/lower/large_objects.mio','bunko/kiso/aot/lower/memory_policy.mio','bunko/kiso/aot/lower/memory_runtime.mio','scripts/native-memory-policy/main.go','scripts/benchmark-native-gc.py','scripts/test_benchmark_native_gc.py','scripts/validate-native-memory.py','scripts/probe-native-memory.py','scripts/report-native-memory.py','benchmarks/native-gc/M19c.md','benchmarks/native-gc/README.md']
 retained=[]
 for directory in sorted((ROOT/'build').glob('m19c-20261025-*')):
  for name in ['result.json','report.json','provenance.json']:
   f=directory/name
   if not f.exists():continue
   d=json.loads(f.read_text());status=d.get('status','BLOCKED')
   retained.append({**ref(str(f.relative_to(ROOT))),'recorded_status':status,'acceptance_status':'BLOCKED' if status=='RUNNING' else ('PASS' if status in ['PASS','BOOTSTRAP_PASS'] else 'FAIL'),'counts_as_final_acceptance':str(f.relative_to(ROOT))==probe_path,'note':'Retained earlier revision, superseded attempt, or interrupted command; never substitutes for final-source checks.'})
 # Hash every recorded M19c log, command record, image and input, including slow
 # samples and failures. No log deletion, sample filtering, or history changes.
 inventory=[]
 for directory in sorted((ROOT/'build').glob('m19c-*')):
  if not directory.is_dir():continue
  for f in sorted(directory.rglob('*')):
   if f.is_file() and f.name not in ['evidence-files.json','report-generation.log']:
    inventory.append({'path':str(f.relative_to(ROOT)),'bytes':f.stat().st_size,'sha256':sha(f)})
 inv=ROOT/'build/m19c-20261026-report/evidence-files.json';inv.write_text(json.dumps(inventory,indent=2)+'\n')
 report={'schema_version':1,'milestone':'M19c','date':'2026-10-26','status':'PASS' if feature_ok else 'BLOCKED','acceptance_scope':'Independent Darwin ARM64 single-threaded M19c functionality; does not accept historical M19b or the overall M19 release.','m19_release_gate':'PASS' if feature_ok and finalrace['status']=='PASS' else 'BLOCKED','modified_files':[ref(p) for p in modified],
 'architecture':{'object_header_bytes':16,'payload_alignment':16,'los_threshold_physical_bytes':131072,'los_prefix_bytes':32,'los_payload_mapping_offset':48,'los_mapping_page_bytes':16384,'los_prefix_words':['all_next','mapping_bytes','young_next','remembered_next'],'normal_side_go':'end+3072+header-origin','normal_side_mio':'end+header-origin','los_side':'header-16','stability':'Arena end, origins, metadata bases and live object addresses never move; mprotect changes page-prefix access only.','generations':'Shared exact layouts, shadow roots, young list, remembered owners and in-place promotion; Young LOS can die in independent Minor.','reclamation':'Zero type after safe sweep; ordinary GC prunes at most 8 LOS mappings per call. Unreleased or failed-release mappings remain owned and charged.','metadata_bytes':3072},
 'budget':{'automatic_opt_in':True,'public_cli_changed':False,'native_system_queries':['sysctl CTL_HW/HW_MEMSIZE before mmap','getrlimit RLIMIT_DATA','getrlimit RLIMIT_AS'],'object_cap_algorithm':'min(requested_limit,(physical_bytes-256MiB)/12,(finite_soft_limit-64KiB)/6), page-rounded; conservative 16MiB hardware-query fallback; fail if no viable budget.','hard_cap':'3*round_page(object_capacity)+64KiB; explicit managed hard cap derives page-rounded object capacity. Both object occupancy and page-charge checks apply.','goal':'min(object_limit,max(1MiB,2*occupied+request_span+min(allocation_rate/20,object_limit/4)))','reserved':'Exact page-rounded virtual mapping lengths including LOS and capacity-sized side metadata.','committed':'Conservative writable managed-page charge: prefix high-water extents including side metadata, fixed-page allowance and full LOS mapping lengths; bounded by reservation. Not an exact OS physical-commit/RSS measurement.','rss':'Per-native-child wait4 peak RSS; separate from managed budget.','growth':'Unused Arena and Side Metadata suffixes PROT_NONE; page-rounded mprotect commits stable prefixes. Startup goal <=1MiB.','shrink':'Goal shrinks after GC; ordinary committed prefixes retain high water; LOS pages are released in bounded batches.','explicit_bootstrap_object_capacity_bytes':1073741824},
 'pressure':{'levels':['Normal','Elevated','High','Critical'],'experimental_usage_thresholds_percent':[70,85,95],'reason_bits':{'capacity':1,'committed_charge':2,'allocation_forecast':4,'low_gc_reclamation':8,'system_pressure':16},'system_query':'kern.memorystatus_vm_pressure_level; unavailable is recorded explicitly.','sampling_minimum_milliseconds':10,'recovery_confirmations':3,'upgrade':'Immediate on policy evaluation','snapshot_fields':['level','reason','heap_used','heap_committed','heap_limit','allocation_rate','gc_active','sequence'],'publication':'Copied native eight-word snapshot; odd/even sequence. Single-threaded ordinary loads/stores; NOT a concurrent-reader atomic protocol.','callbacks_inside_gc':False,'oom_separate':True,'strategy_configuration':['High','Balanced','Low'],'automatic_three_mode_tuning':False},
 'correctness_coverage':[
 {'case':name,'status':'PASS' if feature_ok else 'BLOCKED','evidence':evidence}
 for name,evidence in [
 ('LOS physical threshold below/at/above 128KiB','TestM19cNativeLOS'),
 ('16KiB/128KiB/1MiB/2MiB Storage contents','TestM19cNativeLOS; TestM19cMemoryDifferential/storage-sizes'),
 ('Nonpointer TypedArray does not retain address-shaped values','TestM19cNativeLOS; pure_mio_native_probe'),
 ('Large managed-reference Storage','TestM19cMemoryDifferential/old-los-young'),
 ('Young LOS dies in independent Minor before any Major','TestM19cNativeLOS; pure_mio_native_probe'),
 ('Young LOS survives and promotes without moving','TestM19cNativeLOS; pure_mio_native_probe'),
 ('Old LOS to Young ordinary reference','TestM19cMemoryDifferential/old-los-young'),
 ('Old ordinary owner to Young LOS','TestM19cMemoryDifferential/old-normal-young-los'),
 ('LOS data and aliases across repeated Minor/Major','TestM19cNativeLOS; pure_mio_native_probe'),
 ('Stable addresses through allocation/reclamation','TestM19cNativeLOS; TestM19cNativeMemoryPolicy'),
 ('Stable existing roots during Arena page-prefix growth','TestM19cNativeMemoryPolicy; TestM19cMemoryDifferential/arena-growth'),
 ('Heap Goal growth/shrink','TestM19cNativeMemoryPolicy; adaptive-arena benchmark'),
 ('Explicit managed Hard Cap including metadata','TestM19cNativeOOM; TestM19cNativeMemoryPolicy'),
 ('Major retry then real OOM; existing rooted data retained','TestM19cNativeOOM; TestM19cNativeMemoryPolicy'),
 ('Four pressure levels in actual native code','TestM19cNativeMemoryPolicy; pure_mio_native_probe'),
 ('Immediate upgrade and delayed recovery','TestM19cNativeMemoryPolicy; pure_mio_native_probe'),
 ('Array/Nullable/Cell/Closure/Storage regressions','TestM19bGenerationalDifferential; TestM19cMemoryDifferential; full repository checks'),
 ('Precise roots/shadow chain/GCStress','Native GCStress differential fixtures; full regression; fixed-point GC witness')]],
 'new_oom_gc_race_findings':{'unexpected_oom_or_gc_data_corruption_in_completed_checks':False,'expected_oom_path_verified':True,'race_warning_in_full_log':'WARNING: DATA RACE' in race_text,'full_race_pass':finalrace['status']=='PASS','full_race_is_required_to_clear_release_gate':True},
 'checks':checks,'pure_mio_native_probe':{**ref(probe_path),'status':probe['status'],'sources_unchanged':probe_sources_unchanged,'checks':probe['checks'],'commands':probe['commands'],'image_sha256':probe.get('image_sha256'),'builder_engine':'Go VM with stdlib bindings executes the Mio emitter; emitted Darwin native probe is signed, verified and executed. Separate self-bootstrap validates native execution of pure Mio code generation.'},
 'native_fixed_point':{**ref(fixedpath),'status':'PASS' if fixed['status']=='BOOTSTRAP_PASS' else 'BLOCKED','sources_unchanged':fixed_sources_unchanged,'sha256':fixed['artifacts'].get('stage2',{}).get('sha256'),'stage1_through_stage5_identical':fixed.get('native_fixed_point',False),'stage1_through_stage4_executed':fixed.get('stage4_executed',False),'stage5_continuation':fixed.get('stage5_continuation',False),'go_aot_invocations':fixed.get('go_aot_invocations'),'all_macho_and_strict_codesign_verified':all(a.get('codesign_strict') for a in fixed['artifacts'].values()),'gc_witness_heap_mib':384,'gc_witness':fixed.get('gc_witness'),'historical_m19b_sha256':'db9767775f8d38eb4f44bae64a97658816d34e4ce49f6a2b6e0e3225d06a59bc'},
 'benchmarks':benchmarks,'performance_comparisons':comparisons,'performance_caveat':'All samples retained, fixed-seed interleaving and matched image/input controls. Concurrent validation and host load are recorded. No universal speedup, HTTP p99, production pause SLA or controlled system-memory-exhaustion claim. Small-object allocation overhead and conservative Critical prediction remain visible.',
 'upstream_m19b':{'historical_status':historical['status'],'historical_report':ref('benchmarks/native-gc/m19b-20261021.json'),'race_history':race_history,'current_full_race':finalrace,'timeout_evidence':timeouts,'data_race_warning_seen':'WARNING: DATA RACE' in race_text,'hotspot_evidence':['build/m19c-20261026-report/race-progress-1.txt','build/m19c-20261026-report/race-codegen-1.sample.txt','build/m19c-20261026-report/race-codegen-symbolized-pcln.json','build/m19c-20261026-report/race-progress-2.txt','build/m19c-20261026-report/race-codegen-2-symbolized.json'],'release_blocker':'No valid full-repository Race PASS until one is actually obtained. Historical M19b NOT_ACCEPTED remains unchanged.'},
 'known_limits':['Raw allocation-rate exhaustion prediction is deliberately conservative and can report Critical for sustained collectible churn; production policy calibration remains for M19f.','Committed is a managed-page-charge upper bound, not precise OS physical commitment.','No ordinary Arena page decommit on Goal shrink.','LOS exact-start lookup walks the mapping chain; no concurrent release worker.','Snapshot is not multithread-safe.','No destructive host memory-pressure experiment.','The 22k-live-object growth fixture runs normal mode; GCStress coverage uses the smaller cross-generation, closure, nullable and Storage fixtures.','No fixed-point reuse of historical images: final-source native bootstrap is independently recorded.'],
 'not_run':[{'name':'destructive host/system Critical pressure','status':'NOT RUN'},{'name':'multithread snapshot publication','status':'NOT RUN','reason':'Runtime is single-threaded; future atomic protocol required.'},{'name':'HTTP admission / HTTP p99','status':'NOT RUN','reason':'M19d scope, not implemented.'}],
 'follow_up':{'M19d':'Interpret the read-only pressure snapshot for admission; not implemented.','M19e':'Concurrent Old GC and associated synchronization; not implemented.','M19f':'Three-mode tuning, pressure forecast calibration and allocation-path cost reduction; not implemented.'},'preserved_worktree_audit':ref('build/m19c-20261026-report/worktree-audit.json'),'retained_attempts':retained,'evidence_inventory':ref(str(inv.relative_to(ROOT))),'prohibited_actions_performed':[]}
 output.write_text(json.dumps(report,indent=2)+'\n');print(json.dumps({'status':report['status'],'m19_release_gate':report['m19_release_gate'],'report':str(output)}))
if __name__=='__main__':main()
