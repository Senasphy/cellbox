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
