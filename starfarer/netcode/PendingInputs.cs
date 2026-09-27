namespace Starfarer.Netcode;

public readonly record struct SequencedInput<T>(ulong Sequence, T Value);

// Thread confined. T must be immutable or an owned copy.
public sealed class PendingInputs<T>
{
    private readonly List<SequencedInput<T>> _items = new();
    private readonly int _capacity;
    public ulong LastSequence { get; private set; }
    public ulong LastAcknowledged { get; private set; }
    public int Count => _items.Count;

    public PendingInputs(int capacity)
    {
        if (capacity < 1) throw new ArgumentOutOfRangeException(nameof(capacity));
        _capacity = capacity;
    }

    public bool TryAdd(T value, out SequencedInput<T> input)
    {
        if (_items.Count >= _capacity || LastSequence == ulong.MaxValue) { input = default; return false; }
        input = new(++LastSequence, value);
        _items.Add(input);
        return true;
    }

    public bool Acknowledge(ulong sequence)
    {
        if (sequence < LastAcknowledged || sequence > LastSequence) return false;
        LastAcknowledged = sequence;
        _items.RemoveAll(input => input.Sequence <= sequence);
        return true;
    }

    public SequencedInput<T>[] Oldest(int count)
    {
        if (count < 0) throw new ArgumentOutOfRangeException(nameof(count));
        return _items.Take(count).ToArray();
    }

    // Game supplies the simulation function; netcode never knows movement rules.
    public TState Replay<TState>(TState authoritative,
        Func<TState, SequencedInput<T>, TState> simulate)
    {
        foreach (var input in _items) authoritative = simulate(authoritative, input);
        return authoritative;
    }
}
