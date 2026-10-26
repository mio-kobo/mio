#!/usr/bin/env python3
"""M19c reproducible checks. Never overwrite evidence; never edit history."""
import argparse, hashlib, json, pathlib, platform, subprocess, time
ROOT = pathlib.Path(__file__).resolve().parent.parent
SUITES = {
 "targeted": ["go","test","./internal/aot/...","-run","TestM19|TestM17ePureMioHeap|TestM17ePureMioReadAllCrossPage","-count=1","-v","-timeout=30m"],
 "race": ["go","test","-race","./internal/aot/...","-run","TestM19|TestM17ePureMioHeap|TestM17ePureMioReadAllCrossPage","-count=1","-v","-timeout=30m"],
 "full": ["go","test","./...","-count=1","-timeout=30m"],
 "full-race": ["go","test","-race","./...","-count=1","-timeout=30m"],
 "mio": ["go","run","./cmd/mio","test","./bunko/kiso/aot/..."],
}
def sha(p): return hashlib.sha256(p.read_bytes()).hexdigest()
def main():
 p=argparse.ArgumentParser(description=__doc__);p.add_argument("suite",choices=SUITES);p.add_argument("--evidence",type=pathlib.Path,required=True);args=p.parse_args()
 d=args.evidence.resolve();d.mkdir(parents=True,exist_ok=False)
 files=sorted(set(ROOT.glob("internal/**/*.go"))|set(ROOT.glob("bunko/**/*.mio"))|set(ROOT.glob("selfhost/**/*.mio"))|{pathlib.Path(__file__).resolve()})
 manifest={str(f.relative_to(ROOT)):sha(f) for f in files};(d/"sources.json").write_text(json.dumps(manifest,indent=2)+"\n")
 report={"suite":args.suite,"status":"RUNNING","argv":SUITES[args.suite],"platform":platform.platform(),"started_utc":time.strftime("%Y-%m-%dT%H:%M:%SZ",time.gmtime()),"source_manifest_sha256":sha(d/"sources.json")}
 def save(): (d/"result.json").write_text(json.dumps(report,indent=2)+"\n")
 save();start=time.monotonic()
 with (d/"output.log").open("xb") as log:
  try: result=subprocess.run(report["argv"],cwd=ROOT,stdout=log,stderr=subprocess.STDOUT,timeout=1860);code=result.returncode
  except subprocess.TimeoutExpired: code=124
 report.update(exit_code=code,wall_seconds=time.monotonic()-start,log_sha256=sha(d/"output.log"),sources_unchanged=all(f.exists() and sha(f)==manifest[str(f.relative_to(ROOT))] for f in files))
 report["status"]="PASS" if code==0 and report["sources_unchanged"] else "FAIL"
 save();print(json.dumps(report));return 0 if report["status"]=="PASS" else 1
if __name__=="__main__":raise SystemExit(main())
