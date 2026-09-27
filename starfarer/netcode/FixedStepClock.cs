namespace Starfarer.Netcode;

// Uses elapsed time, never the arrival count of network messages.
public sealed class FixedStepClock
{
    private double _pending;
    public double StepSeconds { get; }
    public int MaxSteps { get; }
    public ulong DroppedSteps { get; private set; }

    public FixedStepClock(double stepSeconds, int maxSteps = 5)
    {
        if (!double.IsFinite(stepSeconds) || stepSeconds <= 0 || maxSteps < 1)
            throw new ArgumentOutOfRangeException(nameof(stepSeconds));
        StepSeconds = stepSeconds;
        MaxSteps = maxSteps;
    }

    public int Advance(double elapsed)
    {
        if (!double.IsFinite(elapsed) || elapsed < 0) throw new ArgumentOutOfRangeException(nameof(elapsed));
        _pending += elapsed;
        int steps = 0;
        while (_pending + 1e-9 >= StepSeconds && steps < MaxSteps)
        { _pending = Math.Max(0, _pending-StepSeconds); steps++; }
        if (_pending >= StepSeconds)
        {
            DroppedSteps += (ulong)(_pending/StepSeconds);
            _pending %= StepSeconds;
        }
        return steps;
    }
}
