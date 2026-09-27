namespace Starfarer.Netcode;

// Thread-confined history on authoritative tick IDs. Inputs/states must be
// immutable values or owned copies; simulate must not produce external effects.
public sealed class PredictionTimeline<TInput, TState>
{
    private readonly SortedDictionary<ulong, TInput> _history = new();
    private readonly Func<TState, TInput, TState> _simulate;
    private readonly int _capacity;
    private TState _authoritative;
    private ulong _resolved;
    public ulong LastTick { get; private set; }
    public int Pending => _history.Count;
    public TState State { get; private set; }
    public PredictionTimeline(TState initial, Func<TState, TInput, TState> simulate, int capacity = 256)
    {
        if (capacity < 1) throw new ArgumentOutOfRangeException(nameof(capacity));
        _simulate = simulate; _capacity = capacity; State = _authoritative = initial;
    }
    public void Reset(ulong tick, TState state)
    { _history.Clear(); _resolved = LastTick = tick; State = _authoritative = state; }
    public void Reconcile(ulong resolved, TState state)
    {
        if (resolved < _resolved) return;
        _resolved = resolved; _authoritative = state;
        foreach (var key in _history.Keys.Where(key => key <= resolved).ToArray()) _history.Remove(key);
        LastTick = Math.Max(LastTick, resolved); Replay();
    }
    public void Generate(ulong target, TInput input, ulong maxCatchUp = 120)
    {
        if (maxCatchUp == 0) throw new ArgumentOutOfRangeException(nameof(maxCatchUp));
        if (target > LastTick && target - LastTick > maxCatchUp)
        { _history.Clear(); LastTick = Math.Max(_resolved, target - 1); }
        while (LastTick < target)
        {
            _history[++LastTick] = input;
            if (_history.Count > _capacity) _history.Remove(_history.Keys.First());
        }
        Replay();
    }
    private void Replay()
    {
        State = _authoritative;
        foreach (var input in _history) State = _simulate(State, input.Value);
    }
    public KeyValuePair<ulong, TInput>[] Recent(int count)
    {
        if (count < 0) throw new ArgumentOutOfRangeException(nameof(count));
        return _history.TakeLast(count).ToArray();
    }
}
