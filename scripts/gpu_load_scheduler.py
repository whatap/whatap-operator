import subprocess
import time
import os
import signal
import sys
import glob
from datetime import datetime

# ================= 설정 구간 =================
# 09:00 ~ 18:00: High Load
# 그 외: Low Load (IDLE_LOAD)
# 1년(31536000초) 동안 실행되도록 설정
LONG_DURATION = 31536000
SCHEDULE = [90, 95, 90, 95, 90, 95, 90, 95, 90, 95]
IDLE_LOAD = 10
CYCLE_DURATION = 60

# dcgmproftester12 실행 커맨드
CMD_BASE = [
    "sudo", "dcgmproftester12",
    "-t", "1004",
    "--no-dcgm-validation"
]
# ========================================================

def cleanup_results():
    """ .results 로 끝나는 불필요한 로그 파일 삭제 """
    try:
        for f in glob.glob("*.results"):
            os.remove(f)
    except Exception as e:
        pass

def get_child_pid(ppid):
    """
    sudo 프로세스의 자식(실제 dcgm 프로세스) PID를 찾습니다.
    /proc 파일시스템을 이용합니다.
    """
    try:
        # /proc/{pid}/task/{pid}/children 파일 읽기 (Linux 3.5+)
        children_path = f"/proc/{ppid}/task/{ppid}/children"
        if os.path.exists(children_path):
            with open(children_path, 'r') as f:
                content = f.read().strip()
                if content:
                    # 첫 번째 자식 PID 반환
                    return int(content.split()[0])
    except Exception as e:
        # 실패 시 부모 PID 그대로 사용 (혹은 로깅)
        print(f"[Warn] Failed to resolve child PID: {e}")
    return ppid

class PersistentLoadGenerator:
    def __init__(self):
        self.process = None
        self.target_pid = None
        self.is_paused = False

    def start(self):
        """프로세스를 백그라운드에서 시작"""
        if self.process and self.process.poll() is None:
            return

        cmd = CMD_BASE + ["-d", str(LONG_DURATION)]
        print(f"[{datetime.now().strftime('%H:%M:%S')}] Starting Process: {' '.join(cmd)}")
        
        # start_new_session=True로 세션 분리 (필요시 그룹 시그널 등 사용 가능)
        self.process = subprocess.Popen(
            cmd, 
            stdout=subprocess.DEVNULL, 
            stderr=subprocess.DEVNULL,
            start_new_session=True
        )
        
        # 프로세스가 뜨고 PID가 잡힐 때까지 잠시 대기
        time.sleep(1)
        
        # sudo를 사용한 경우, 실제 자식 프로세스 PID를 찾아야 함
        # sudo에게 SIGSTOP을 보내도 자식에게 전파되지 않을 수 있음
        if "sudo" in CMD_BASE[0]:
            self.target_pid = get_child_pid(self.process.pid)
            if self.target_pid != self.process.pid:
                print(f"[{datetime.now().strftime('%H:%M:%S')}] Resolved PID: sudo({self.process.pid}) -> child({self.target_pid})")
            else:
                print(f"[{datetime.now().strftime('%H:%M:%S')}] Using PID: {self.process.pid} (Could not resolve child or not running via sudo wrapper)")
        else:
            self.target_pid = self.process.pid

        self.is_paused = False

    def _send_signal(self, sig):
        """타겟 PID(실제 프로세스)에 시그널 전송"""
        if not self.target_pid:
            return
        
        try:
            # sudo 권한 문제로 os.kill이 실패할 수 있음.
            # 이 경우 sudo kill 명령어를 사용
            os.kill(self.target_pid, sig)
        except PermissionError:
            # 파이썬 스크립트가 root가 아니면 직접 시그널을 못 보낼 수 있음 -> sudo kill 사용
            sig_map = {signal.SIGSTOP: "-STOP", signal.SIGCONT: "-CONT", signal.SIGTERM: "-TERM"}
            sig_flag = sig_map.get(sig)
            if sig_flag:
                subprocess.run(["sudo", "kill", sig_flag, str(self.target_pid)], 
                               stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        except ProcessLookupError:
            print(f"Process {self.target_pid} not found. Restarting...")
            self.start()

    def pause(self):
        if not self.is_paused and self.process and self.process.poll() is None:
            self._send_signal(signal.SIGSTOP)
            self.is_paused = True

    def resume(self):
        if self.is_paused and self.process and self.process.poll() is None:
            self._send_signal(signal.SIGCONT)
            self.is_paused = False

    def stop(self):
        """완전 종료"""
        if self.process:
            print(f"[{datetime.now().strftime('%H:%M:%S')}] Stopping process...")
            # 멈춰있던 프로세스라면 먼저 깨워야 kill이 잘 먹힘
            if self.is_paused:
                self.resume()
                time.sleep(0.1)
            
            self.process.terminate()
            try:
                self.process.wait(timeout=3)
            except subprocess.TimeoutExpired:
                self.process.kill()
            
            self.process = None
            self.target_pid = None

    def run_cycle(self, target_percent):
        """60초(CYCLE_DURATION) 동안 목표 부하 비율에 맞춰 Pause/Resume 반복"""
        
        # 프로세스 죽었으면 재시작
        if self.process is None or self.process.poll() is not None:
            print(f"[{datetime.now().strftime('%H:%M:%S')}] Process died, restarting...")
            self.start()

        # 1. 계산
        run_time = CYCLE_DURATION * (target_percent / 100.0)
        sleep_time = CYCLE_DURATION - run_time

        current_time_str = datetime.now().strftime('%H:%M:%S')
        print(f"[{current_time_str}] Target: {target_percent}% | Run: {run_time:.1f}s | Sleep: {sleep_time:.1f}s")

        # 2. 실행 (Resume -> Sleep)
        if run_time > 0:
            self.resume()
            # 정확한 타이밍을 위해
            start_run = time.time()
            # run_time만큼 대기 (하지만 로직 실행 시간 보정은 복잡하니 단순 sleep)
            time.sleep(run_time)
        
        # 3. 휴식 (Pause -> Sleep)
        if sleep_time > 0:
            # target_percent가 100이면 pause 불필요
            if target_percent < 100:
                self.pause()
            
            time.sleep(sleep_time)

        # 주기적으로 결과 파일 정리
        cleanup_results()

def main():
    print(f"=== DGX Persistent Load Scheduler (SIGSTOP/CONT Mode) ===")
    print(f"Pattern: {SCHEDULE}")
    print(f"Cycle Duration: {CYCLE_DURATION}s")

    generator = PersistentLoadGenerator()
    
    # 종료 시그널 처리 (Ctrl+C 등)
    def signal_handler(sig, frame):
        print("\nExiting...")
        generator.stop()
        sys.exit(0)
    
    signal.signal(signal.SIGINT, signal_handler)
    signal.signal(signal.SIGTERM, signal_handler)

    try:
        generator.start()

        while True:
            now = datetime.now()
            current_hour = now.hour

            if 9 <= current_hour <= 18:
                index = current_hour - 9
                target_load = SCHEDULE[index] if 0 <= index < len(SCHEDULE) else SCHEDULE[-1]
            else:
                target_load = IDLE_LOAD

            generator.run_cycle(target_load)

    finally:
        generator.stop()

if __name__ == "__main__":
    main()
