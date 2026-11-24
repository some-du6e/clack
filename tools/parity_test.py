#!/usr/bin/env python3
"""tools/parity.py's own tests. They never run go: the go test results are
synthetic go test -json events.

  python3 -I tools/parity_test.py
"""
import contextlib
import importlib.util
import io
import json
import os
import re
import sys
import tempfile
import unittest

sys.dont_write_bytecode = True  # no tools/__pycache__ (-I ignores PYTHONDONTWRITEBYTECODE)
HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location('parity', os.path.join(HERE, 'parity.py'))
parity = importlib.util.module_from_spec(spec)
spec.loader.exec_module(parity)

MOD = 'example.test/m'


def ev(action, test='', pkg='p', output=None, **kw):
    e = {'Action': action, 'Package': f'{MOD}/{pkg}'}
    if test:
        e['Test'] = test
    if output is not None:
        e['Output'] = output
    e.update(kw)
    return e


def write(path, text):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, 'w', encoding='utf-8') as f:
        f.write(text)


SLACK = {'kind': 'slack-js', 'source': 'a module', 'build': 'b1', 'captured': '2026-11-14'}


class FakeRepo:
    """A repo with one package p holding TestSnap (a snapshot) and TestPlain."""

    def __init__(self, cases, refresh=None):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = self.tmp.name
        write(os.path.join(self.root, 'go.mod'), f'module {MOD}\n')
        write(os.path.join(self.root, 'p', 'p_test.go'),
              'package p\n\nfunc TestSnap(t *testing.T) {}\n\nfunc TestPlain(t *testing.T) {}\n'
              '\nfunc TestRulesLikeSlack(t *testing.T) {}\n')
        slack = dict(SLACK, **({'refresh': refresh} if refresh else {}))
        write(os.path.join(self.root, 'p', 'testdata', 'parity', 'd.json'),
              json.dumps({'dimension': 'd', 'slack': slack, 'cases': cases}))
        self.reg = {
            'areas': [{'id': 'a', 'name': 'Area A'}, {'id': 'b', 'name': 'Area B', 'audit': 'docs/B.md'}],
            'dimensions': [{'id': 'd', 'area': 'a', 'tier': 'product', 'what': 'D.', 'checks': [
                {'package': 'p', 'test': 'TestSnap', 'snapshot': 'p/testdata/parity/d.json', 'compares': 'Rows.'},
                {'package': 'p', 'test': 'TestPlain', 'compares': 'Values.', 'oracle': SLACK},
            ]}],
            'deliberate': [], 'elsewhere': [{'area': 'b', 'workflow': 'b.yml', 'what': 'B pictures.'}],
        }

    def report(self, events):
        r = parity.collect(events)
        return parity.report(self.reg, r, MOD, {'what': 'test', 'events': r.events}, 'abc', root=self.root)

    def close(self):
        self.tmp.cleanup()


CASES = [
    {'name': 'same one', 'in': 1, 'slack': ['a']},
    {'name': 'differs', 'in': 2, 'slack': ['a', 'b']},
    {'name': 'known one', 'in': 3, 'slack': ['x'], 'known': 'set differs', 'clack': ['y']},
    {'name': 'no result', 'in': 4, 'slack': ['z']},
]


def snap_events(plain='pass'):
    return [
        ev('run', 'TestSnap'),
        ev('run', 'TestSnap/same_one'), ev('pass', 'TestSnap/same_one'),
        ev('run', 'TestSnap/differs'),
        ev('output', 'TestSnap/differs', output='    paritytest.go:99: parity: differs from Slack\n'),
        ev('output', 'TestSnap/differs', output='        slack: ["a","b"]\n'),
        ev('output', 'TestSnap/differs', output='        clack: ["a","c"]\n'),
        ev('output', 'TestSnap/differs', output='--- FAIL: TestSnap/differs (0.00s)\n'),
        ev('fail', 'TestSnap/differs'),
        ev('run', 'TestSnap/known_one'),
        ev('output', 'TestSnap/known_one', output='    paritytest.go:90: parity: known divergence: set differs\n'),
        ev('output', 'TestSnap/known_one', output='        slack: ["x"]\n'),
        ev('output', 'TestSnap/known_one', output='        clack: ["y"]\n'),
        ev('skip', 'TestSnap/known_one'),
        ev('fail', 'TestSnap'),
        ev('run', 'TestPlain'), ev(plain, 'TestPlain'),
        ev('fail', ''),
    ]


class ParityTests(unittest.TestCase):
    def test_the_registry_and_its_snapshots(self):
        reg = parity.load_registry()
        for d, c in parity.checks_of(reg):
            self.assertTrue(parity.has_test_func(parity.ROOT, c['package'], c['test']),
                            f'{d["id"]}: no func {c["test"]} in {c["package"]}')
        product = [d for d in reg['dimensions'] if d['tier'] == 'product']
        self.assertTrue(product)
        # every snapshot case a known divergence marks says why and what Slack gives
        for d, c in parity.checks_of(reg):
            if c.get('snapshot'):
                s = parity.read_json(os.path.join(parity.ROOT, c['snapshot']))
                self.assertEqual(s['dimension'], c['snapshot'].rsplit('/', 1)[1][:-5])
                for case in s['cases']:
                    if case.get('known'):
                        self.assertIsNotNone(case.get('slack'), case['name'])

    def test_area_docs_exist_and_audits_are_scored(self):
        reg = parity.load_registry()
        with open(os.path.join(parity.ROOT, 'docs', 'gaps', 'score.py'), encoding='utf-8') as f:
            score = f.read()
        for a in reg['areas']:
            for key in ('audit', 'docs'):
                if a.get(key):
                    self.assertTrue(os.path.exists(os.path.join(parity.ROOT, a[key])), a[key])
            if a.get('audit'):
                stem = os.path.basename(a['audit'])[:-3]
                # the report calls it "manual audit (score.py)": score.py must read it
                self.assertTrue(f"'{stem[:-6]}'" in score if stem.endswith('-AUDIT') else a['audit'] in score,
                                f'{a["id"]}: score.py does not read {a["audit"]}; put it under "docs"')

    def test_validate_finds_mistakes(self):
        repo = FakeRepo(CASES)
        try:
            reg = json.loads(json.dumps(repo.reg))
            reg['dimensions'][0]['checks'].append(dict(reg['dimensions'][0]['checks'][1]))
            reg['dimensions'][0]['checks'][1]['oracle'] = {'kind': 'reading the code', 'source': 'x', 'captured': 'y'}
            reg['dimensions'][0]['area'] = 'nowhere'
            errs = '\n'.join(parity.validate(reg, repo.root))
            self.assertIn('registered twice', errs)
            self.assertIn("kind 'reading the code'", errs)
            self.assertIn('unknown area nowhere', errs)
            self.assertEqual(parity.validate(repo.reg, repo.root), [])
            write(os.path.join(repo.root, 'p', 'testdata', 'parity', 'd.json'), json.dumps(
                {'dimension': 'd', 'slack': SLACK, 'cases': [{'name': 'a b', 'in': 1}, {'name': 'a_b', 'in': 2},
                                                             {'name': 'x/y', 'in': 3}]}))
            errs = '\n'.join(parity.validate(repo.reg, repo.root))
            self.assertIn("'a_b' is empty, holds a /, or repeats", errs)
            self.assertIn("'x/y'", errs)
        finally:
            repo.close()

    def test_go_name_is_testings_rewrite(self):
        self.assertEqual(parity.go_name('tu cold'), 'tu_cold')
        self.assertEqual(parity.go_name('like: keywords'), 'like:_keywords')
        self.assertEqual(parity.go_name('a\tb'), 'a_b')
        self.assertEqual(parity.go_name('café ¯\\_(ツ)_/¯'), 'café_¯\\_(ツ)_/¯')

    def test_a_value_split_over_output_events_reads_whole(self):
        # go test -json splits a long line over several output events, mid-token
        events = ['    paritytest.go:117: parity: differs from Slack\n', '        slack: {"rows": ["a: Add', ' reaction…", "b"]}\n',
                  '        clack: {"rows": ["a: Add reaction…"]}\n']
        self.assertEqual(parity.values(parity.own_lines(events)),
                         {'slack': {'rows': ['a: Add reaction…', 'b']}, 'clack': {'rows': ['a: Add reaction…']}})

    def test_each_case_gets_its_own_answer(self):
        repo = FakeRepo(CASES)
        try:
            r = repo.report(snap_events())
            d = r['dimensions'][0]
            snap, plain = d['checks']
            got = {c['name']: c['status'] for c in snap['cases']}
            self.assertEqual(got, {'same one': 'match', 'differs': 'diverge', 'known one': 'known',
                                   'no result': 'not-run'})
            differs = next(c for c in snap['cases'] if c['name'] == 'differs')
            self.assertEqual(differs['slack'], ['a', 'b'])
            self.assertEqual(differs['clack'], ['a', 'c'])
            self.assertEqual(snap['status'], 'diverge')
            self.assertEqual(plain['status'], 'match')
            self.assertEqual(d['status'], 'diverge', 'the worst check decides')
            text = '\n'.join(parity.blunt(r))
            self.assertIn('row 2: Slack b, Clack c', text)
            self.assertIn('known divergence', text.replace('known difference', 'known divergence'))
        finally:
            repo.close()

    def test_a_failed_assert_beside_the_comparison_is_not_a_divergence(self):
        repo = FakeRepo([{'name': 'one', 'in': 1, 'slack': ['a']}])
        try:
            r = repo.report([ev('run', 'TestSnap/one'),
                             ev('output', 'TestSnap/one', output='    x_test.go:3: 4 lines for "a"\n'),
                             ev('fail', 'TestSnap/one'), ev('fail', 'TestSnap'), ev('pass', 'TestPlain')])
            snap = r['dimensions'][0]['checks'][0]
            self.assertEqual(snap['cases'][0]['status'], 'fail')
            self.assertEqual(snap['status'], 'fail')
            self.assertEqual(r['totals']['checks_diverge'], 0)
        finally:
            repo.close()

    def test_known_divergences_and_pending_cases_count_as_not_the_same(self):
        repo = FakeRepo([{'name': 'k', 'in': 1, 'slack': ['x'], 'known': 'why', 'clack': ['y']},
                         {'name': 'ok', 'in': 2, 'slack': ['a']}])
        try:
            events = [ev('output', 'TestSnap/k', output='    p.go:1: parity: known divergence: why\n'),
                      ev('skip', 'TestSnap/k'), ev('pass', 'TestSnap/ok'), ev('pass', 'TestSnap'), ev('pass', 'TestPlain')]
            r = repo.report(events)
            self.assertEqual(r['dimensions'][0]['status'], 'known')
            self.assertEqual(r['totals']['dimensions_match'], 0)
            case = r['dimensions'][0]['checks'][0]['cases'][0]
            self.assertEqual((case['why'], case['recorded']), ('why', ['y']))
            events[0] = ev('output', 'TestSnap/k', output='    p.go:1: parity: no Slack output for this case yet\n')
            self.assertEqual(repo.report(events)['dimensions'][0]['status'], 'not-run')
        finally:
            repo.close()

    def test_unmeasured_checks_say_why(self):
        repo = FakeRepo(CASES)
        try:
            r = repo.report([ev('pass', 'TestSnap/same_one')])
            self.assertEqual(r['dimensions'][0]['checks'][1]['status'], 'not-run')
            self.assertIn('ran, but not TestPlain', r['dimensions'][0]['checks'][1]['why'])
            r = repo.report([])
            self.assertIn('no results for p', r['dimensions'][0]['checks'][0]['why'])
            build = [{'ImportPath': f'{MOD}/p [{MOD}/p.test]', 'Action': 'build-output', 'Output': 'p_test.go:1: undefined: x\n'},
                     {'ImportPath': f'{MOD}/p [{MOD}/p.test]', 'Action': 'build-fail'},
                     ev('fail', '', FailedBuild=f'{MOD}/p [{MOD}/p.test]')]
            c = repo.report(build)['dimensions'][0]['checks'][0]
            self.assertIn('did not build', c['why'])
            self.assertIn('undefined: x', c['output'])
            repo.reg['dimensions'][0]['checks'][1]['test'] = 'TestRenamed'
            self.assertIn('renamed or removed', repo.report([])['dimensions'][0]['checks'][1]['why'])
        finally:
            repo.close()

    def test_the_report_never_claims_more_than_it_measured(self):
        repo = FakeRepo([{'name': 'one', 'in': 1, 'slack': ['a']}])
        try:
            r = repo.report([ev('pass', 'TestSnap/one'), ev('pass', 'TestSnap'), ev('pass', 'TestPlain')])
            self.assertEqual(r['dimensions'][0]['status'], 'match')
            text = '\n'.join(parity.blunt(r))
            md = parity.markdown(r, parity.slack_named_unregistered(repo.reg, repo.root))
            self.assertIn('1 of 1 dimensions the same as Slack', text)
            self.assertIn('1 of the 2 areas of Slack\'s client have no check at all', text)
            self.assertIn('not a parity percentage', text)
            self.assertIn('not covered: Area B', text)
            self.assertIn('## Not covered', md)
            self.assertIn('`b.yml`', md)
            self.assertIn('p TestRulesLikeSlack', md, 'Slack-named tests that are not registered are listed')
            # no percentage anywhere: a rosy "N% parity" badge is what this report is not
            self.assertIsNone(re.search(r'\d\s*%', text + md), 'a percentage crept into the report')
        finally:
            repo.close()

    def test_real_report_has_no_percentage(self):
        reg = parity.load_registry()
        r = parity.report(reg, parity.collect([]), parity.module_path(), {'what': 'nothing', 'events': 0})
        self.assertEqual(r['totals']['dimensions_match'], 0, 'nothing ran, so nothing is the same')
        self.assertIsNone(re.search(r'\d\s*%', '\n'.join(parity.blunt(r)) + parity.markdown(r, [])))

    def test_refresh_rewrites_only_slacks_side(self):
        repo = FakeRepo([{'name': 'stale', 'in': 1, 'slack': ['old'], 'known': 'k', 'clack': ['c']},
                         {'name': 'fine', 'in': 2, 'slack': ['same']},
                         {'name': 'new', 'in': 3, 'slack': None},
                         {'name': 'kept', 'in': 4, 'slack': ['k'], 'known': 'k2', 'clack': ['c2']},
                         {'name': 'moved', 'in': 5, 'slack': ['a']}], refresh='fake')
        try:
            seen = []

            def fake(cases, tmp):
                seen.append([c['in'] for c in cases])
                return {'stale': ['now'], 'fine': ['same'], 'new': ['added'], 'kept': ['k'], 'moved': ['b']}

            path = os.path.join(repo.root, 'p', 'testdata', 'parity', 'd.json')
            out = io.StringIO()
            with contextlib.redirect_stdout(out):
                code = parity.refresh(repo.reg, {'d'}, dry_run=True, root=repo.root, refreshers={'fake': fake})
            self.assertEqual(code, 3, 'a known divergence whose Slack side changed fails the refresh')
            self.assertIn('stale: Slack CHANGED', out.getvalue())
            self.assertIn('its known divergence is dropped', out.getvalue())
            self.assertIn('new: new', out.getvalue())
            self.assertEqual(parity.read_json(path)['cases'][0]['slack'], ['old'], 'a dry run writes nothing')
            with contextlib.redirect_stdout(io.StringIO()):
                code = parity.refresh(repo.reg, {'d'}, root=repo.root, refreshers={'fake': fake}, today='2026-12-01')
            self.assertEqual(code, 3)
            s = parity.read_json(path)
            self.assertEqual([c['slack'] for c in s['cases']], [['now'], ['same'], ['added'], ['k'], ['b']])
            self.assertNotIn('known', s['cases'][0], 'the old reason was about the old Slack side')
            self.assertNotIn('clack', s['cases'][0])
            self.assertEqual((s['cases'][3]['known'], s['cases'][3]['clack']), ('k2', ['c2']), 'an unchanged one keeps its mark')
            self.assertEqual(s['slack']['captured'], '2026-12-01')
            self.assertEqual(seen, [[1, 2, 3, 4, 5], [1, 2, 3, 4, 5]])
            with contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(parity.refresh(repo.reg, {'d'}, root=repo.root, refreshers={'fake': fake}), 0,
                                 'once written, nothing changed: the refresh passes')
            with contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(parity.refresh(repo.reg, {'d'}, root=repo.root, refreshers={}), 2,
                                 'no refresher wired up for it')
        finally:
            repo.close()

    def test_emoji_frecency_matches_the_go_test(self):
        self.assertEqual(parity.duration_ms('5m'), 300000)
        self.assertEqual(parity.duration_ms('48h'), 172800000)
        self.assertEqual(parity.duration_ms('1h30m'), 5400000)
        with self.assertRaises(ValueError):
            parity.duration_ms('2d')
        # the visits the Go test builds from tSlackNow (2026-11-14 12:00 UTC)
        got = parity.emoji_frecency([{'key': 'E1f44d', 'id': 'E1f44d', 'count': 400, 'ago': ['5m', '2h']}])
        self.assertEqual(got, {'E1f44d': {'id': 'E1f44d', 'count': 400, 'visits': [1794657300000, 1794650400000]}})

    def test_reads_go_test_json_files(self):
        with tempfile.TemporaryDirectory() as tmp:
            write(os.path.join(tmp, 'shard', 'rest.jsonl'),
                  'not json\n' + json.dumps(ev('pass', 'TestA')) + '\n{"broken"\n' + json.dumps(ev('pass', '')) + '\n')
            r = parity.collect(parity.read_events([tmp]))
            self.assertEqual(r.events, 2)
            self.assertEqual(r.tests[(f'{MOD}/p', 'TestA')]['status'], 'pass')

    def test_a_failure_wins_over_a_pass_when_a_test_ran_twice(self):
        r = parity.collect([ev('pass', 'TestA'), ev('fail', 'TestA'), ev('pass', 'TestA')])
        self.assertEqual(r.tests[(f'{MOD}/p', 'TestA')]['status'], 'fail')

    def test_worst_status_wins(self):
        self.assertEqual(parity.worst(['match', 'known']), 'known')
        self.assertEqual(parity.worst(['known', 'diverge', 'not-run']), 'diverge')
        self.assertEqual(parity.worst([]), 'not-run')


if __name__ == '__main__':
    unittest.main()
