#!/usr/bin/env python3
"""Exercise real CLI, MCP and HTTP reads against an isolated temporary service."""
import argparse
import json
import os
import pathlib
import selectors
import subprocess
import tempfile
import urllib.error
import urllib.parse
import urllib.request


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--tb', required=True)
    parser.add_argument('--mcp', required=True)
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix='taskboard-agent-read-') as temp:
        workspace = pathlib.Path(temp) / '工作目录 with spaces'
        workspace.mkdir()
        service = subprocess.Popen([args.tb, 'serve', '--addr', '127.0.0.1:0', '--db', str(pathlib.Path(temp) / 'test.db')], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        mcp = None
        try:
            selector = selectors.DefaultSelector()
            selector.register(service.stdout, selectors.EVENT_READ)
            assert selector.select(timeout=10), 'temporary service did not announce its address'
            line = service.stdout.readline()
            assert 'listening on ' in line, line
            addr = line.split('listening on ', 1)[1].split()[0]
            base = 'http://' + addr

            def cli(*command):
                result = subprocess.run([args.tb, '--addr', addr, *command], text=True, capture_output=True, timeout=15)
                assert result.returncode == 0, result.stderr
                return result.stdout

            def http(path, body=None):
                req = urllib.request.Request(base + path, data=None if body is None else json.dumps(body).encode(), headers={'Content-Type': 'application/json', 'X-Agent': 'isolated-agent-read-check'})
                with urllib.request.urlopen(req, timeout=10) as response:
                    return json.load(response)

            project = json.loads(cli('project', 'add', '读取验收', '--path', str(workspace), '--json'))
            assert project['workspacePath'] == str(workspace)
            assert any(p['id'] == project['id'] and p['workspacePath'] == str(workspace) for p in json.loads(cli('projects', '--json')))
            column = project['columns'][0]
            tasks = [http('/api/tasks', {'projectId': project['id'], 'columnId': column['id'], 'title': f'任务 {i}', 'description': '完整正文 ' + str(i), 'assignee': 'luna' if i < 2 else 'other'}) for i in range(3)]
            http('/api/tasks/' + tasks[1]['id'] + '/comments', {'text': '验收标准与补充上下文'})
            summary = json.loads(cli('list', '--project', project['id'], '--column', column['name'], '--assignee', 'luna', '--limit', '1', '--offset', '1', '--summary', '--json'))
            assert len(summary) == 1 and summary[0]['id'] == tasks[1]['id']
            assert not {'description', 'comments', 'position', 'archived'} & summary[0].keys()
            detail = json.loads(cli('show', tasks[1]['id'], '--json'))
            assert detail['description'] == '完整正文 1'
            assert detail['comments'][0]['text'] == '验收标准与补充上下文'
            human = cli('show', tasks[1]['id'])
            assert '完整正文 1' in human and '验收标准与补充上下文' in human
            assert cli('list', '--project', project['id']).count('任务 ') == 3
            assert json.loads(cli('list', '--project', project['id'], '--assignee', 'nobody', '--json')) == []

            env = {**os.environ, 'TASKBOARD_ADDR': addr, 'TASKBOARD_AGENT': 'isolated-agent-read-check'}
            mcp = subprocess.Popen([args.mcp], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, env=env)
            number = 0

            def rpc(method, params=None):
                nonlocal number
                number += 1
                mcp.stdin.write(json.dumps({'jsonrpc': '2.0', 'id': number, 'method': method, 'params': params or {}}) + '\n')
                mcp.stdin.flush()
                ready = selectors.DefaultSelector()
                ready.register(mcp.stdout, selectors.EVENT_READ)
                assert ready.select(timeout=10), 'MCP response timeout'
                response = json.loads(mcp.stdout.readline())
                assert 'error' not in response, response
                return response['result']

            def tool(name, arguments):
                response = rpc('tools/call', {'name': name, 'arguments': arguments})
                assert not response.get('isError'), response
                return json.loads(response['content'][0]['text'])

            rpc('initialize', {'protocolVersion': '2025-06-18', 'capabilities': {}, 'clientInfo': {'name': 'isolated-check', 'version': '1'}})
            names = {t['name'] for t in rpc('tools/list')['tools']}
            assert {'get_task', 'create_project', 'list_tasks', 'list_projects'} <= names
            assert tool('list_tasks', {'projectId': project['id'], 'columnId': column['id'], 'assignee': 'luna', 'limit': 1, 'offset': 1, 'summary': True}) == summary
            assert tool('get_task', {'id': tasks[1]['id']}) == detail
            mcp_project = tool('create_project', {'name': 'MCP 项目', 'workspacePath': str(workspace)})
            assert mcp_project['workspacePath'] == str(workspace)
            assert any(p['id'] == mcp_project['id'] and p['workspacePath'] == str(workspace) for p in tool('list_projects', {}))
            assert http('/api/projects/' + project['id'] + '/board')['project']['workspacePath'] == str(workspace)
            for suffix in ['&limit=0', '&limit=201', '&offset=1', '&summary=yes', '&columnId=does-not-exist']:
                try:
                    http('/api/tasks?projectId=' + project['id'] + suffix)
                except urllib.error.HTTPError as error:
                    assert error.code == 400, (suffix, error.code)
                else:
                    raise AssertionError('invalid query accepted: ' + suffix)
            print(json.dumps({'result': 'PASS', 'checks': ['CLI 项目路径与 JSON', '摘要过滤与分页', '单卡正文及评论', '旧列表与空结果', 'MCP 读取闭环', 'MCP 项目创建', 'HTTP 参数校验'], 'database': 'isolated temporary database'}, ensure_ascii=False))
        finally:
            if mcp is not None:
                mcp.terminate()
                mcp.wait(timeout=10)
            service.terminate()
            service.wait(timeout=10)


if __name__ == '__main__':
    main()
