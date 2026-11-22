from cellbox_tools import _clip, dispatch

def test_clip_keeps_head_tail_and_caps_result():
    value, omitted = _clip(b"a" * 40_000, cap=128)
    assert len(value.encode()) <= 128
    assert "omitted" in value
    assert omitted > 39_000

def test_command_result_includes_status_and_bounded_streams():
    class Result:
        exit_code=124;duration_ms=17;state="timed_out";stdout="x"*40_000;stderr="warning"
    class Session:
        def exec(self,*args,**kwargs):return Result()
    result=dispatch(Session(),"run_command",{"command":["sleep","1"]})
    assert result["exit_code"]==124
    assert result["duration_ms"]==17
    assert result["timeout_or_oom_reason"]=="timeout"
    assert len(result["stdout"].encode())<=16*1024
    assert result["omitted_bytes"]["stdout"]>0

def test_command_result_reports_native_oom_state():
    class Result:
        exit_code=None;duration_ms=17;state="oom_killed";stdout="";stderr=""
    class Session:
        def exec(self,*args,**kwargs):return Result()
    result=dispatch(Session(),"run_command",{"command":["memory-bomb"]})
    assert result["timeout_or_oom_reason"]=="oom"

def test_snapshot_and_fork_dispatch():
    class Child:
        id="ses_child"
        data={"parent_snapshot_id":"snp_base"}
    class Session:
        def snapshot(self,**kwargs):
            assert kwargs=={"labels":{"purpose":"eval"}}
            return {"id":"snp_base"}
        def fork(self,snapshot_id=None):
            assert snapshot_id=="snp_base"
            return Child()
    session=Session()
    assert dispatch(session,"snapshot",{"labels":{"purpose":"eval"}})=={"id":"snp_base"}
    assert dispatch(session,"fork",{"snapshot_id":"snp_base"})=={"session_id":"ses_child","parent_snapshot_id":"snp_base"}
