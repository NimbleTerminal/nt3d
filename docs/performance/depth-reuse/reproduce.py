#!/usr/bin/env python3
"""Run sequential paired benchmarks on supplied, disposable source copies.

See README.md for revisions and the identical camera compatibility adjustment.
Neither supplied module files nor production dependency pins are changed.
"""
import argparse
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

p = argparse.ArgumentParser(description=__doc__)
for name in ('nt3d-before', 'nt3d-after', 'charts-before', 'charts-after', 'output'):
    p.add_argument('--' + name, required=True, type=Path)
a = p.parse_args()
a.output.mkdir(parents=True, exist_ok=True)
artifacts = Path(__file__).resolve().parent
with tempfile.TemporaryDirectory(prefix='nt3d-depth-') as temp:
    for variant in ('before', 'after'):
        charts = getattr(a, 'charts_' + variant).resolve()
        engine = getattr(a, 'nt3d_' + variant).resolve()
        # These copies are disposable: install identical measurement code.
        for source, target in [('depth_bench_test.go.txt', 'depth_bench_test.go'),
                               ('pixel_oracle_test.go.txt', 'depth_pixel_oracle_test.go')]:
            shutil.copyfile(artifacts / source, charts / target)
        workspace = Path(temp) / (variant + '.work')
        workspace.write_text(f'go 1.26.8\nuse (\n "{charts}"\n "{engine}"\n)\n')
        env = dict(os.environ, GOWORK=str(workspace))
        with (a.output / (variant + '.txt')).open('w') as log:
            def run(args):
                log.write('$ ' + ' '.join(args) + '\n')
                log.flush()
                subprocess.run(args, cwd=charts, env=env, stdout=log,
                               stderr=subprocess.STDOUT, check=True)
            run(['go', 'version'])
            run(['go', 'list', '-m', 'all'])
            run(['go', 'test', '-run', '^TestDepthPixelOracle$', '-count=1', '-v', '.'])
            for benchmark in ('^BenchmarkSoftwareRendererLifetime$',
                              '^BenchmarkDepthSnapshots$',
                              '^BenchmarkPresentation/(glyph|direct)/(camera|texture|bars|burst)$'):
                run(['go', 'test', '-run', '^$', '-bench', benchmark,
                     '-benchmem', '-benchtime=72x', '-count=3', '.'])
            # Isolate shm samples so the sink cannot exhaust pending shm limits.
            for work in ('camera', 'texture', 'bars', 'burst'):
                for _ in range(3):
                    run(['go', 'test', '-run', '^$', '-bench',
                         '^BenchmarkPresentation/shm/' + work + '$',
                         '-benchmem', '-benchtime=20x', '-count=1', '.'])
        print(variant + ' complete', flush=True)
