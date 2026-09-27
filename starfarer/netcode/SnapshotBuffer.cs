namespace Starfarer.Netcode;

// Timestamp units are seconds on one monotonic timeline, supplied by the caller.
public sealed class SnapshotBuffer<T>
{
    private readonly record struct Sample(double Time, T Value);
    private readonly List<Sample> _samples = new();
    private readonly int _capacity;

    public SnapshotBuffer(int capacity = 32)
    {
        if (capacity < 2) throw new ArgumentOutOfRangeException(nameof(capacity));
        _capacity = capacity;
    }

    public bool Add(double time, T value)
    {
        if (!double.IsFinite(time)) return false;
        if (_samples.Count > 0)
        {
            if (time < _samples[^1].Time) return false;
            if (time == _samples[^1].Time) { _samples[^1] = new(time, value); return true; }
        }
        _samples.Add(new(time, value));
        if (_samples.Count > _capacity) _samples.RemoveAt(0);
        return true;
    }

    public bool TrySample(double time, Func<T, T, float, T> interpolate, out T value)
    {
        if (_samples.Count == 0 || !double.IsFinite(time)) { value = default!; return false; }
        while (_samples.Count >= 2 && _samples[1].Time <= time) _samples.RemoveAt(0);
        if (_samples.Count == 1) { value = _samples[0].Value; return true; }
        var a = _samples[0];
        var b = _samples[1];
        float weight = (float)Math.Clamp((time-a.Time)/(b.Time-a.Time), 0, 1);
        value = interpolate(a.Value, b.Value, weight);
        return true;
    }
}
