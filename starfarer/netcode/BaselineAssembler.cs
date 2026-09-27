namespace Starfarer.Netcode;

// A missing fragment never becomes a partial visibility set. Newer complete
// baselines supersede older ones; bounded incomplete sets expire independently.
public sealed class BaselineAssembler<T>
{
    private sealed record Assembly(double Created, double Time, T[][] Parts);
    private readonly Dictionary<ulong, Assembly> _pending = new();
    public ulong LastApplied { get; private set; }
    public int Pending => _pending.Count;
    public void Reset() { _pending.Clear(); LastApplied = 0; }
    public T[]? Add(ulong tick, int index, int count, double time, double now, T[] items)
    {
        foreach (var key in _pending.Where(pair => now - pair.Value.Created > 2).Select(pair => pair.Key).ToArray()) _pending.Remove(key);
        if (tick <= LastApplied || count is < 1 or > 8 || index < 0 || index >= count || items.Length > 20 || !double.IsFinite(time)) return null;
        if (!_pending.TryGetValue(tick, out var assembly))
        {
            if (_pending.Count >= 8) _pending.Remove(_pending.Keys.Min());
            _pending[tick] = assembly = new(now, time, new T[count][]);
        }
        if (assembly.Parts.Length != count || assembly.Time != time) return null;
        assembly.Parts[index] ??= items;
        if (assembly.Parts.Any(part => part == null)) return null;
        var result = assembly.Parts.SelectMany(part => part).ToArray();
        if (result.Length > 128) { _pending.Remove(tick); return null; }
        LastApplied = tick;
        foreach (var old in _pending.Keys.Where(key => key <= tick).ToArray()) _pending.Remove(old);
        return result;
    }
}
